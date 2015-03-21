package main

import (
	"machinedetector/mdpb"
	"machinedetector/server"
	"github.com/golang/protobuf/proto"
	"github.com/golang/glog"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("Need Root Privilege To Run")
		os.Exit(1)
	}

	// Find binary path
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(dir)

	// Cd to binary path
	err = os.Chdir(dir)
	if err != nil {
		log.Fatal(err)
	}

	// Load conf
	bytearr, err := ioutil.ReadFile("../conf/md.conf")
	if err != nil {
		log.Fatal(err)
	}
	stringcontent := string(bytearr)

	pProgConf := new(mdpb.Conf)
	err = proto.UnmarshalText(stringcontent, pProgConf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print(proto.MarshalTextString(pProgConf))

	os.MkdirAll("../log", 0755)
	glog.SetLogDir("../log")
	*(pProgConf.LogDir) = dir + "/../log/"

	// Create the server
	s := server.New(pProgConf)

	s.Start()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	// Block until a signal is received.
	sig := <-c
	glog.Infof("Trapped Signal; %v", sig)

	// Stop the service gracefully.
	s.Stop()
	glog.Info("Gracefully Quit!")
	glog.Flush()
	os.Exit(0)
}
