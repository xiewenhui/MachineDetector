package server

import (
	"machinedetector/mdpb"
  "github.com/golang/protobuf/proto"
	"github.com/garyburd/redigo/redis"
	"github.com/golang/glog"
	"io/ioutil"
	"os"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var redisIndex int = 0

func GetHosts(mc *RedisConn, bc *RedisConn, dbname string) ([]string, error) {
	hostlistkey := HostListKey

	var redisConns [2]*RedisConn = [2]*RedisConn{mc, bc}
	redisIndex++
	i := redisIndex % 2
	if redisIndex > 99999 {
		redisIndex = 0
	}

	reply, err := redisConns[i].SafeDo("GET", hostlistkey)
	if err != nil {
		reply, err = redisConns[1-i].SafeDo("GET", hostlistkey)
		if err != nil {
			return nil, err
		}
	}

	byteslice, err := redis.Bytes(reply, nil)
	pPkt := new(mdpb.StringVec)
	err = proto.Unmarshal(byteslice, pPkt)
	if err != nil {
		glog.Warningf("ParseFromStringError for [%s", hostlistkey)
		return nil, err
	}

	glog.Infof("Datasource [%s:%d:%s]", pPkt.GetDbHost(), pPkt.GetDbPort(), pPkt.GetDbName())
	if pPkt.GetDbHost() != dbname {
		glog.Warningf("[%s] data may be polluted by [%s]", hostlistkey, pPkt.GetLocalhost())
		return nil, fmt.Errorf("[%s] data may be polluted by [%s]", hostlistkey, pPkt.GetLocalhost())
	}

	// Allocates a slice of length 0 and capacity HostNum
	// hosts := make([]string, 0, HostNum)
	return pPkt.GetStrs(), nil
}

func GetAllIsps(mc *RedisConn, bc *RedisConn, dbname string) (map[string]int, error) {
	m := make(map[string]int)
	hn, err := os.Hostname()
	if err != nil {
		glog.Warningf("os.Hostname() error %v", err)
	} else {
		m[hn] = 1
	}

	inspectorKey := InspectorHashMap

	var redisConns [2]*RedisConn = [2]*RedisConn{mc, bc}
	redisIndex++
	i := redisIndex % 2
	if redisIndex > 99999 {
		redisIndex = 0
	}

	reply, err := redisConns[i].SafeDo("HGETALL", inspectorKey)
	if err != nil {
		reply, err = redisConns[1-i].SafeDo("HGETALL", inspectorKey)
		if err != nil {
			return m, nil
		}
	}

	fields, err := redis.Values(reply, nil)
	n := len(fields)
	if n < 2 || n%2 != 0 {
		glog.Warningf("len(fields)=%d", n)
		return m, nil
	}

	for i := 0; i < n; i += 2 {
		glog.Infof("%s => %s", fields[i], fields[i+1])
		key, _ := redis.String(fields[i], nil)
		value, _ := redis.String(fields[i+1], nil)
		sps := strings.Split(value, "_")
		if len(sps) != 2 || sps[1] != dbname {
			glog.Warningf("[%s] been Attack? [%s] [%s]", inspectorKey, key, value)
			continue
		}
		ts, err := strconv.Atoi(sps[0])
		if err != nil {
			glog.Warningf("Parse [%s] to int faild, [%s] [%s]", sps[0], key, value)
			continue
		}
		if time.Now().Unix()-int64(ts) > InspectorTolerateOff {
			glog.Infof("[%s] is Offline", key)
			continue
		}
		glog.Infof("[%s] is Ok", key)
		m[key] = 1
	}

	return m, nil
}

// https://github.com/garyburd/redigo/issues/21
func SetHostStats(mc *RedisConn, bc *RedisConn, hlist []string) {
	t := time.Now().Unix()
	args := []interface{}{MaybeDetectedDeadHost}
	for _, v := range hlist {
		args = append(args, v, t)
	}
	if _, err := mc.SafeDo("HMSET", args...); err != nil {
		glog.Warningf("SetHostStats Failed, [%s] is down?", mc.HostPort())
	} else {
		glog.Infof("SetHostStats Ok, [%s]", mc.HostPort())
	}
	if _, err := bc.SafeDo("HMSET", args...); err != nil {
		glog.Warningf("SetHostStats Failed, [%s] is down?", bc.HostPort())
	} else {
		glog.Infof("SetHostStats Ok, [%s]", bc.HostPort())
	}
}

func UnlinkOldFile(dir string, ttlSec int64) {
	files, err := ioutil.ReadDir(dir)
	t := time.Now()
	if err != nil {
		glog.Warningf("ReadDir [%s] error[%v]", dir, err)
		return
	}
	for _, f := range files {
		if f.Mode().IsRegular() == false {
			glog.Infof("[%s] Not Regular, Continue", f.Name())
			continue
		}
		if t.Unix()-f.ModTime().Unix() > ttlSec {
			filename := dir + f.Name()
			glog.Infof("Try to Remove [%s]", filename)
			if err = os.Remove(filename); err != nil {
				glog.Warningf("Remove [%s] failed [%v]", filename, err)
			}
		}
	}
}
