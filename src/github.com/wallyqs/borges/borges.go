/**
 *	(The MIT License)
 *
 *  Copyright (c) 2015 Waldemar Quevedo. All rights reserved.
 *
 * Permission is hereby granted, free of charge, to any person
 *  obtaining a copy of this software and associated documentation
 *  files (the "Software"), to deal in the Software without
 *  restriction, including without limitation the rights to use, copy,
 *  modify, merge, publish, distribute, sublicense, and/or sell copies
 *  of the Software, and to permit persons to whom the Software is
 *  furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be
 * included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
 * NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS
 * BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN
 * ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
 * CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package main

import (
	"flag"
	"fmt"
	// "strconv"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/gogo/protobuf/proto"
	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
	"github.com/satori/go.uuid"
	// util  "github.com/mesos/mesos-go/mesosutil"
	sched "github.com/mesos/mesos-go/scheduler"
	org "github.com/wallyqs/org-go"
)

const BORGES_VERSION = "0.0.1"

const (
	MIN_CPUS_PER_TASK = 1
	MIN_MEM_PER_TASK  = 56
)

var (
	setupfile = flag.String("f", "", "Setup file for Borges in Org mode")
)

func init() {
	flag.Parse()
}

type BorgesScheduler struct {
	Scheduler       *BorgesScheduler
	CodeBlocks      map[*mesos.TaskID]*mesos.TaskInfo
	CodeBlocksQueue []*org.OrgSrcBlock
}

func (sched *BorgesScheduler) ResourceOffers(driver sched.SchedulerDriver, offers []*mesos.Offer) {}

func (sched *BorgesScheduler) StatusUpdate(driver sched.SchedulerDriver, status *mesos.TaskStatus) {}

func (sched *BorgesScheduler) Registered(driver sched.SchedulerDriver, frameworkId *mesos.FrameworkID, masterInfo *mesos.MasterInfo) {
}
func (sched *BorgesScheduler) Reregistered(driver sched.SchedulerDriver, masterInfo *mesos.MasterInfo) {
}
func (sched *BorgesScheduler) Disconnected(sched.SchedulerDriver)                   {}
func (sched *BorgesScheduler) OfferRescinded(sched.SchedulerDriver, *mesos.OfferID) {}

func (sched *BorgesScheduler) FrameworkMessage(sched.SchedulerDriver, *mesos.ExecutorID, *mesos.SlaveID, string) {
}
func (sched *BorgesScheduler) SlaveLost(sched.SchedulerDriver, *mesos.SlaveID) {}
func (sched *BorgesScheduler) ExecutorLost(sched.SchedulerDriver, *mesos.ExecutorID, *mesos.SlaveID, int) {
}
func (sched *BorgesScheduler) Error(driver sched.SchedulerDriver, err string) {}

// Takes a blockname and returns a Mesos task with an uuid
//
func NewCodeBlockTask(blockname string) *mesos.TaskInfo {
	tuuid := uuid.NewV4()
	task := &mesos.TaskInfo{
		Name: proto.String(blockname + "/" + tuuid.String()),
		TaskId: &mesos.TaskID{
			Value: proto.String(tuuid.String()),
		},
	}

	return task
}

type BorgesAPIServer struct {
	Scheduler *BorgesScheduler
	Server    *http.Server
	Bind      string
	Listener  net.Listener
}

func (s *BorgesAPIServer) OrgHandler(w http.ResponseWriter, r *http.Request) {
	log.Infoln("POST /org")

	switch r.Method {
	case "POST":
		defer r.Body.Close()
		contents, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Fatal("Can't listen to the monitor port: %v", err)
		}
		orgtext := string(contents)

		blocks := getBlocksFromString(orgtext)
		for _, block := range blocks {
			s.Scheduler.CodeBlocksQueue = append(s.Scheduler.CodeBlocksQueue, block)
		}
		fmt.Fprintf(w, "Scheduled %v code blocks for execution", len(blocks))

	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Not found: %v /org/", r.Method)
	}

}

func getBlocksFromString(orgtext string) []*org.OrgSrcBlock {
	blocks := make([]*org.OrgSrcBlock, 0)

	// Ugh...
	root := org.Preprocess(orgtext)
	tokens := org.Tokenize(orgtext, root)

	for _, t := range tokens {
		switch o := t.(type) {
		case *org.OrgSrcBlock:
			blocks = append(blocks, o)
		}
	}

	return blocks
}

func (s *BorgesAPIServer) RootHandler(w http.ResponseWriter, r *http.Request) {
	log.Infoln("GET /")
	fmt.Fprintf(w, "Borges Scheduler v%s", BORGES_VERSION)
}

func (s *BorgesAPIServer) HealthzHandler(w http.ResponseWriter, r *http.Request) {
	log.Infoln("GET /healthz")
	fmt.Fprintf(w, "OK\n")
}

func (s *BorgesAPIServer) VarzHandler(w http.ResponseWriter, r *http.Request) {
	log.Infoln("GET /varz")
	fmt.Fprintf(w, "TODO: GET /varz")
}

func NewAPIServer(bind string) *BorgesAPIServer {

	l, err := net.Listen("tcp", bind)
	if err != nil {
		log.Fatal("Can't listen to the monitor port: %v", err)
	}

	api := &BorgesAPIServer{
		Bind:     bind,
		Listener: l,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", api.RootHandler)
	mux.HandleFunc("/org", api.OrgHandler)
	mux.HandleFunc("/healthz", api.HealthzHandler)
	mux.HandleFunc("/varz", api.VarzHandler)

	api.Server = &http.Server{
		Addr:           bind,
		Handler:        mux,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	return api
}

func (s *BorgesAPIServer) Start() {
	log.Infoln("API server running at ", s.Bind)
	s.Server.Serve(s.Listener)
}

// Borrowed from the mesos-go example at:
// https://github.com/mesos/mesos-go/blob/master/examples/test_framework.go#L235
func parseIP(address string) net.IP {
	addr, err := net.LookupIP(address)
	if err != nil {
		log.Infoln(err)
	}
	if len(addr) < 1 {
		fmt.Printf("failed to parse IP from address '%v'", address)
	}
	return addr[0]
}

func main() {

	// Parse Org mode file first and get the code blocks that will be run
	//
	log.Infoln("Reading #+setupfile: ", *setupfile)
	contents, err := ioutil.ReadFile(*setupfile)
	if err != nil {
		fmt.Printf("Problem reading the file: %v \n", err)
	}
	config := org.Preprocess(string(contents))

	// Create Scheduler and HTTP API server
	//
	borges := &BorgesScheduler{
		CodeBlocks:      make(map[*mesos.TaskID]*mesos.TaskInfo),
		CodeBlocksQueue: make([]*org.OrgSrcBlock, 0),
	}
	server := NewAPIServer(config.Settings["BORGES_BIND"] + ":" + config.Settings["BORGES_PORT"])
	server.Scheduler = borges

	// Configure the Driver
	//
	bindingAddress := parseIP(config.Settings["BORGES_BIND"])
	driverConfig := sched.DriverConfig{
		Scheduler: borges,
		Framework: &mesos.FrameworkInfo{
			User: proto.String(""), // covered by the mesos-go bindings
			Name: proto.String("Borges v" + BORGES_VERSION),
		},
		Master:         config.Settings["MESOS_MASTER"],
		BindingAddress: bindingAddress,
	}

	driver, err := sched.NewMesosSchedulerDriver(driverConfig)
	if err != nil {
		log.Infoln("Unable to create a SchedulerDriver ", err.Error())
	}

	// Start HTTP Server
	//
	go func() { server.Start() }()

	// Start Mesos Scheduler
	//
	if stat, err := driver.Run(); err != nil {
		fmt.Printf("Framework stopped with status %s and error: %s\n", stat.String(), err.Error())
	}

}
