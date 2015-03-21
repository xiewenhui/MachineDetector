package server

import (
	"fmt"
	"github.com/garyburd/redigo/redis"
	"github.com/golang/glog"
	"time"
)

type RedisConn struct {
	host        string
	port        int32
	hp          string
	connTimeOut int32
	sockTimeOut int32
	database    string
	passwd      string
	ctx         redis.Conn // is a interface
}

func (rc *RedisConn) HostPort() string {
  return rc.hp
}

func NewRedisConn(h string,
	pt int32,
	ct int32,
	st int32,
	db string,
	pd string) *RedisConn {

	rc := &RedisConn{
		host:        h,
		port:        pt,
		connTimeOut: ct,
		sockTimeOut: st,
		database:    db,
		passwd:      pd,
	}

	rc.hp = h + fmt.Sprintf(":%d", pt)
	rc.ctx = nil
	return rc
}

func (rc *RedisConn) DisConnect() {
	if rc.ctx != nil {
		rc.ctx.Close()
		rc.ctx = nil
	}
}

func (rc *RedisConn) DoConnect() error {
	var err error = nil
	rc.DisConnect()
	rc.ctx, err = redis.DialTimeout("tcp",
		rc.hp,
		time.Duration(rc.connTimeOut)*time.Millisecond,
		time.Duration(rc.sockTimeOut)*time.Millisecond,
		time.Duration(rc.sockTimeOut)*time.Millisecond)

	if rc.ctx == nil || err != nil {
		glog.Warningf("connect [%s] error %v", rc.hp, err)
		return err
	}
	_, err = rc.ctx.Do("AUTH", rc.passwd)
	if err != nil {
		glog.Warningf("auth [%s] error %v", rc.hp, err)
		rc.ctx.Close()
		rc.ctx = nil
		return err
	}
	_, err = rc.ctx.Do("SELECT", rc.database)
	if err != nil {
		glog.Warningf("select [%s][%s] error %v", rc.hp, rc.database, err)
		rc.ctx.Close()
		rc.ctx = nil
		return err
	}
	return nil
}

func (rc *RedisConn) Do(cmd string, args ...interface{}) (interface{}, error) {
	if rc.ctx == nil {
		err := rc.DoConnect()
		if err != nil {
			return nil, err
		}
	}
	return rc.ctx.Do(cmd, args...)
}

func (rc *RedisConn) SafeDo(cmd string, args ...interface{}) (interface{}, error) {
	reply, err := rc.Do(cmd, args...)
	if err == nil {
		return reply, err
	}
	err = rc.DoConnect()
	if err != nil {
		return nil, err
	}
	return rc.Do(cmd, args...)
}
