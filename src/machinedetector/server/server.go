package server

import (
  "machinedetector/mdpb"
	"machinedetector/ping"
	"machinedetector/ssh"

	"encoding/json"
	"fmt"
	"github.com/garyburd/redigo/redis"
	"github.com/golang/glog"
	"github.com/stathat/consistent"
	"github.com/golang/protobuf/proto"
	_ "io/ioutil"
	"net"
	"net/http"
	"os"
	_ "os/signal"
	"reflect"
	"strconv"
	"sync"
	"time"

	// Allow dynamic profiling.
	_ "net/http/pprof"
)

// Need to export the fields so that the json package can see it.
type InspectorInfo struct {
	StartTimeStr     string
	StartTimeStamp   int64
	LastInsTimeStr   string
	LastInsTimeStamp int64
	LastTimeCost     int64
	Inspectors       []string `json:"inspectors"`
	DetectedHosts    []string `json:"myPartitionDetectedHosts"`
	PingDownHosts    []string `json:"pingDownHosts"`
	SshDownHosts     []string `json:"sshDownHosts"`
	AllDownHosts     []string `json:"allDownHosts"`
	TotalNum         int      `json:"totalMaxHostNum"`
	DetectedNum      int      `json:"myPartitionDetectedNum"`
	PingDownNum      int      `json:"pingDownNum"`
	SshDownNum       int      `json:"sshDownNum"`
	AllDownNum       int      `json:"allDownNum"`
}

type Inspector struct {
	loopTimes int
	opts      *mdpb.Conf
	mr        *RedisConn
	br        *RedisConn
	running   bool
	tokens    chan int
	allHosts  []string
	allIsps   map[string]int
	hostState []int

	mutex   *sync.Mutex
	info    *InspectorInfo
	infostr []byte
}

func NewInspector(opts *mdpb.Conf) *Inspector {
	isp := &Inspector{
		opts:    opts,
		running: false,
		tokens:  make(chan int, opts.GetConCurrency()),
		allIsps: make(map[string]int),
		mutex:   &sync.Mutex{},
		info:    new(InspectorInfo),
		infostr: []byte{'{', '}'},
	}
	isp.mr = NewRedisConn(opts.GetMasterRedis(), opts.GetRedisPort(),
		opts.GetRedisConnTimeOut(), opts.GetRedisSockTimeOut(),
		opts.GetRedisDatabase(), opts.GetRedisPasswd())

	isp.br = NewRedisConn(opts.GetBackupRedis(), opts.GetRedisPort(),
		opts.GetRedisConnTimeOut(), opts.GetRedisSockTimeOut(),
		opts.GetRedisDatabase(), opts.GetRedisPasswd())

	isp.info.PingDownHosts = make([]string, 0, MaxHostNum)
	isp.info.SshDownHosts = make([]string, 0, MaxHostNum)
	isp.info.AllDownHosts = make([]string, 0, MaxHostNum)
	isp.info.DetectedHosts = make([]string, 0, MaxHostNum)
	isp.info.Inspectors = []string{}

	return isp
}

func (isp *Inspector) Stop() {
	isp.running = false
}

func (isp *Inspector) Run() {
	glog.Info("Inspector Start")
	isp.running = true

	t := time.Now()
	isp.info.StartTimeStamp = t.Unix()
	isp.info.StartTimeStr = fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())

OLoop:
	for {
		if isp.running == false {
			break OLoop
		}

		hostlistChanged := false
		inspectorlistChanged := false

		if (isp.loopTimes % 13) == 0 {
			// Get hostlist from redis
			currHosts, err := GetHosts(isp.mr, isp.br, isp.opts.GetDbHosts())
			if err != nil && len(isp.allHosts) == 0 {
				glog.Fatalf("Can't Get AllHostList First %v", err)
			}
			if err == nil {
				isp.allHosts = currHosts
				hostlistChanged = true
			} else {
				glog.Warningf("Get AllHostList failed, Use Last")
			}
		}
		glog.Infof("loop[%d] : hostlist size=[%d], hostlistChanged=%v", isp.loopTimes, len(isp.allHosts), hostlistChanged)

		// Get Inspector list
		// for k := range m {
		//  delete(m, k)
		// }
		var err error
		nowAllIsps, err := GetAllIsps(isp.mr, isp.br, isp.opts.GetDbHosts())
		if reflect.DeepEqual(isp.allIsps, nowAllIsps) == false {
			isp.allIsps = nowAllIsps
			inspectorlistChanged = true
			glog.Infof("loop[%d] : InspectorList Changed: %v", isp.loopTimes, isp.allIsps)
		} else {
			glog.Infof("loop[%d] : InspectorList No Change: %v", isp.loopTimes, isp.allIsps)
		}

		if len(isp.allIsps) == 0 {
			glog.Fatalf("Inspector list is empty")
		}

		if hostlistChanged || inspectorlistChanged {
			// Calculate myPartition use consistent hash ring
			hashring := consistent.New()
			for k, v := range isp.allIsps {
				glog.Infof("%s => %d", k, v)
				hashring.Add(k)
			}

			hn, _ := os.Hostname()
			// Dispatch using consistent hash
			isp.info.DetectedHosts = isp.info.DetectedHosts[0:0]
			for _, h := range isp.allHosts {
				server, err := hashring.Get(h)
				if err != nil {
					isp.info.DetectedHosts = append(isp.info.DetectedHosts, h)
					glog.Warning(err)
					continue
				}
				if server == hn {
					isp.info.DetectedHosts = append(isp.info.DetectedHosts, h)
				}
			}
		}
		glog.Infof("loop[%d] : MyPartition Size=[%d]", isp.loopTimes, len(isp.info.DetectedHosts))

		// Start ping And ssh
		glog.Infof("loop[%d] : Ping Started", isp.loopTimes)
		isp.hostState = make([]int, len(isp.info.DetectedHosts), len(isp.info.DetectedHosts))
		var wg sync.WaitGroup

		for i, h := range isp.info.DetectedHosts {
			if isp.running == false {
				break
			}
			isp.tokens <- 1
			wg.Add(1)
			go func(ii int, hh string) {
				defer wg.Done()
				alive := ping.Ping(hh, int(isp.opts.GetPingTimeOutMs()))
				<-isp.tokens
				if alive == false {
					isp.hostState[ii] = 1
				}
			}(i, h)
		}
		wg.Wait()
		glog.Infof("loop[%d] : Ping End", isp.loopTimes)

		if isp.running == false {
			break OLoop
		}

		glog.Infof("loop[%d] : Ssh Start", isp.loopTimes)
		for i, h := range isp.info.DetectedHosts {
			if isp.running == false {
				break
			}
			isp.tokens <- 1
			wg.Add(1)
			go func(ii int, hh string) {
				defer wg.Done()
				alive := ssh.Ssh(hh, int(isp.opts.GetSshTimeOutMs()))
				<-isp.tokens
				if alive == false {
					isp.hostState[ii] += 2
				}
			}(i, h)
		}
		wg.Wait()
		glog.Infof("loop[%d] : Ssh End", isp.loopTimes)

		if isp.running == false {
			break OLoop
		}

		// Dump State
		var pingDownNum int
		var sshDownNum int
		var allDownNum int
		isp.info.PingDownHosts = isp.info.PingDownHosts[0:0]
		isp.info.SshDownHosts = isp.info.SshDownHosts[0:0]
		isp.info.AllDownHosts = isp.info.AllDownHosts[0:0]
		for i, h := range isp.info.DetectedHosts {
			if isp.hostState[i] == 1 {
				glog.Infof("ping down :[%d][%s]", i, h)
				pingDownNum++
				isp.info.PingDownHosts = append(isp.info.PingDownHosts, h)
			} else if isp.hostState[i] == 2 {
				glog.Infof("ssh down :[%s]", h)
				sshDownNum++
				isp.info.SshDownHosts = append(isp.info.SshDownHosts, h)
			} else if isp.hostState[i] == 3 {
				glog.Infof("ping And ssh down :[%s]", h)
				allDownNum++
				isp.info.AllDownHosts = append(isp.info.AllDownHosts, h)
			}
		}
		glog.Infof("loop[%d] : pingDownNum=[%d], sshDownNum[%d], allDownNum=[%d]",
			isp.loopTimes, pingDownNum, sshDownNum, allDownNum)

		SetHostStats(isp.mr, isp.br, isp.info.AllDownHosts)

		isp.info.TotalNum = len(isp.allHosts)
		isp.info.DetectedNum = len(isp.info.DetectedHosts)
		isp.info.PingDownNum = pingDownNum
		isp.info.SshDownNum = sshDownNum
		isp.info.AllDownNum = allDownNum

		isp.info.Inspectors = make([]string, 0, len(isp.allIsps))
		for k, _ := range isp.allIsps {
			isp.info.Inspectors = append(isp.info.Inspectors, k)
		}

		t := time.Now()
		isp.info.LastTimeCost = t.Unix() - isp.info.LastInsTimeStamp
		isp.info.LastInsTimeStamp = t.Unix()
		isp.info.LastInsTimeStr = fmt.Sprintf("%d-%02d-%02d %02d:%02d:%02d",
			t.Year(), t.Month(), t.Day(),
			t.Hour(), t.Minute(), t.Second())

		// b, err := json.Marshal(*(isp.info))
		b, err := json.MarshalIndent(isp.info, "", "  ")
		if err != nil {
			glog.Warningf("loop[%d] : json.Marshal Info failed [%v]", isp.loopTimes, err)
		}
		glog.Infof("loop[%d] : json.Marshal Info len [%d]", isp.loopTimes, len(b))

		isp.loopTimes++
		isp.mutex.Lock()
		isp.infostr = b
		isp.mutex.Unlock()
	}

	glog.Info("Inspector Stop")
}

func (isp *Inspector) Dump() []byte {
	isp.mutex.Lock()
	defer isp.mutex.Unlock()
	return isp.infostr
}

// Server is our main struct
type Server struct {
	start     time.Time
	opts      *mdpb.Conf
	http      net.Listener
	done      chan bool
	waitGroup *sync.WaitGroup
	mTicker   *time.Ticker
	bTicker   *time.Ticker
	isp       *Inspector
	logTicker *time.Ticker
}

// New will setup a new server struct
func New(opts *mdpb.Conf) *Server {
	s := &Server{
		start:     time.Now(),
		opts:      opts,
		done:      make(chan bool),
		waitGroup: &sync.WaitGroup{},
	}

	s.mTicker = time.NewTicker(time.Millisecond * InspectorHbPeriodMs)
	s.bTicker = time.NewTicker(time.Millisecond * InspectorHbPeriodMs)
	s.logTicker = time.NewTicker(time.Second * LogKeepIntev)
	s.isp = NewInspector(opts)

	return s
}

func (s *Server) Start() {
	go s.StartHeartBeat(s.opts.GetMasterRedis(), s.mTicker)
	go s.StartHeartBeat(s.opts.GetBackupRedis(), s.bTicker)
	go s.RunInspector()
	go s.StartHTTPMonitoring()
	go s.StartLogTicker()
}

func (s *Server) Stop() {
	s.http.Close()
	s.mTicker.Stop()
	s.bTicker.Stop()
	s.logTicker.Stop()
	s.StopInspector()
	close(s.done)
	s.waitGroup.Wait()
}

func (s *Server) RunInspector() {
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	s.isp.Run()
}

func (s *Server) StopInspector() {
	s.isp.Stop()
}

// StartHTTPMonitoring will enable the HTTP monitoring port.
func (s *Server) StartHTTPMonitoring() {
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	glog.Infof("Starting http monitor on port %d", s.opts.GetStatusPort())

	hp := fmt.Sprintf(":%d", s.opts.GetStatusPort())

	l, err := net.Listen("tcp", hp)
	if err != nil {
		glog.Fatalf("Can't listen to the monitor port: %v", err)
	}

	mux := http.NewServeMux()

	// Varz
	mux.HandleFunc("/conf", s.HandleConf)
	mux.HandleFunc("/version", s.HandleVersion)
	mux.HandleFunc("/stat", s.HandleStat)

	srv := &http.Server{
		Addr:           hp,
		Handler:        mux,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	s.http = l

	srv.Serve(s.http)
	glog.Infof("Exiting http monitor on port %d", s.opts.GetStatusPort())
}

func (s *Server) StartLogTicker() {
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	glog.Infof("Start LogTicker")

	for {
		select {
		case <-s.done:
			glog.Info("Stopping LogTicker")
			return
		case <-s.logTicker.C:
			glog.Info("UnlinkOldFile ...")
			UnlinkOldFile(s.opts.GetLogDir(), s.opts.GetLogLiveTime())
		}
	}
}

// register self to redis
// https://godoc.org/github.com/garyburd/redigo/redis
func (s *Server) StartHeartBeat(rh string, ticker *time.Ticker) {
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	rp := fmt.Sprintf(":%d", s.opts.GetRedisPort())

	for {
		select {
		case <-s.done:
			glog.Infof("stopping heartbeat to [%s%s]", rh, rp)
			return
		case <-ticker.C:
			hn, err := os.Hostname()
			if err != nil {
				glog.Warningf("os.Hostname() error %v", err)
				continue
			}
			c, err := redis.DialTimeout("tcp",
				rh+rp,
				time.Duration(s.opts.GetRedisConnTimeOut())*time.Millisecond,
				time.Duration(s.opts.GetRedisSockTimeOut())*time.Millisecond,
				time.Duration(s.opts.GetRedisSockTimeOut())*time.Millisecond)

			if c == nil || err != nil {
				glog.Warningf("retry connect [%s%s] error %v", rh, rp, err)
				continue
			}

			_, _ = c.Do("AUTH", s.opts.GetRedisPasswd())
			_, _ = c.Do("SELECT", s.opts.GetRedisDatabase())
			_, _ = c.Do("HSET", InspectorHashMap, hn, strconv.FormatInt(time.Now().Unix(), 10)+"_"+s.opts.GetDbHosts())
			glog.Infof("[%s] heartbeating to [%s%s]", hn, rh, rp)
			c.Close()
		}
	}
}

func (s *Server) HandleConf(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(proto.MarshalTextString(s.opts)))
}

func (s *Server) HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(VERSION + "\n"))
}

func (s *Server) HandleStat(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(s.isp.Dump()))
}
