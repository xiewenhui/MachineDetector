package ssh

import (
	"github.com/golang/glog"
	"net"
	"time"
)

func Ssh(address string, timeoutMs int) bool {
	c, err := net.DialTimeout("tcp", address+":22", time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		glog.Warningf("Connect timeout [%s],[%v]", address, err)
		return false
	}
	defer c.Close()
	return true

	c.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))

	var buf [5]byte
	_, err = c.Read(buf[:])
	if err != nil {
		glog.Warningf("Read timeout [%s]", address)
		return false
	}
	glog.Infof("Ssh [%s] return [%s]", address, buf)
	return true
}
