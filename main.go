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

func getMasterAddr() {
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

	m := strings.Split(out.String(), "\n")

	mux.Lock()
	redisMaster = fmt.Sprintf("%s:%s", m[0], m[1])
	if redisMaster != oldMaster {
		log.Println("redis master addr", redisMaster)
		oldMaster = redisMaster
	}
	mux.Unlock()
}

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
		getMasterAddr()
		mux.Lock()
		masterAddr = redisMaster
		mux.Unlock()
		masterConn, err = net.DialTimeout("tcp", masterAddr, time.Duration(1*time.Second))
	}

	if err != nil {
		log.Println(err)
		return
	}
	// proxy !
	go dataCopy(masterConn, client)
	go dataCopy(client, masterConn)
}

func dataCopy(from, to net.Conn) {
	defer from.Close()
	io.Copy(from, to)
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

	// be sure that master address is
	// (yes, it's not perfect, but it can save some slave/master confusion problems when redis cluster is completly restarted)
	go func() {
		for {
			select {
			case <-time.Tick(1 * time.Second):
				getMasterAddr()
			}
		}
	}()

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
