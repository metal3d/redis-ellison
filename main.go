package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	mux          = &sync.Mutex{}
	clusterName  = "mymaster"
	redisCommand = "redis-cli"
	sentinelHost = "127.0.0.1"
	sentinelPort = "26379"
	redisMaster  = ""
	oldMaster    = ""
	maxTries     = 10
	triesDelay   = time.Duration(1 * time.Second)
)

// Contact sentinel and get master address. Set it in redisMaster global variable.
// That function makes use of mutex to avoid race condition.
func refreshMasterAddr() {

	// this is the command line to call redis sentinel to get master address.
	command := strings.Split(redisCommand, " ")
	command = append(command,
		"-h", sentinelHost,
		"-p", sentinelPort,
		"SENTINEL", "get-master-addr-by-name", clusterName)
	cmd := exec.Command(command[0], command[1:]...)

	var out bytes.Buffer
	var er bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &er

	if err := cmd.Start(); err != nil {
		log.Println(err)
		log.Println("err::", er.String())
		return
	}
	cmd.Wait()

	// We should get 2 lines, one for master up and the second for port.
	m := strings.Split(out.String(), "\n")

	if len(m) > 1 {
		mux.Lock()
		redisMaster = fmt.Sprintf("%s:%s", m[0], m[1])
		if redisMaster != oldMaster {
			log.Println("redis master addr", redisMaster)
			oldMaster = redisMaster
		}
		mux.Unlock()
	}
}

// Handles the client connection to bind it to the current master.
func handleConn(client net.Conn) {
	var masterConn net.Conn
	var err error

	// first attempt
	mux.Lock()
	masterAddr := redisMaster
	mux.Unlock()
	masterConn, err = net.DialTimeout("tcp", masterAddr, time.Duration(1*time.Second))

	// note that this loop will not happend if err is nil
	for i := 0; i < maxTries && err != nil; i++ {
		time.Sleep(triesDelay)
		refreshMasterAddr()
		mux.Lock()
		masterAddr = redisMaster
		mux.Unlock()
		masterConn, err = net.DialTimeout("tcp", masterAddr, time.Duration(1*time.Second))
	}

	if err != nil {
		log.Println(err)
		return
	}

	// Two ways data copy
	go dataCopy(masterConn, client)
	go dataCopy(client, masterConn)
}

// Full copy of client->server or server->client.
// Close the "from" part when copy reaches EOF.
func dataCopy(from, to net.Conn) {
	defer from.Close()
	io.Copy(from, to)
}

// Auto refresh master address in memory
func backgroundAutoRefresh() {
	for {
		select {
		case <-time.Tick(1 * time.Second):
			refreshMasterAddr()
		}
	}
}

func main() {
	flag.StringVar(&clusterName, "cluster", clusterName,
		"redis cluster name")
	flag.StringVar(&redisCommand, "redis-command", redisCommand,
		"redis command to connect to sentinel, it can also be \"docker exec sentinel1 redis-cli\" for example")
	flag.StringVar(&sentinelHost, "sentinel-host", sentinelHost,
		"sentinel host or ip")
	flag.StringVar(&sentinelPort, "sentinel-port", sentinelPort,
		"sentinel port to connect")
	flag.Parse()
	overFlag()

	// be sure that master address is up to date
	go backgroundAutoRefresh()

	// now, wait for clients and handle connections to the master
	c, err := net.Listen("tcp", ":6379")
	if err != nil {
		log.Fatal(err)
	}
	for {
		conn, err := c.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go handleConn(conn)
	}
}

// override flag name by their environment variable if found.
func overFlag() {
	flag.VisitAll(func(f *flag.Flag) {
		env := strings.ToUpper(f.Name)
		env = strings.Replace(env, "-", "_", -1)
		if v := os.Getenv(env); v != "" {
			flag.Set(f.Name, v)
		}
	})
}
