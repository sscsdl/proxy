package main

import (
	"flag"
	"fmt"
	"net"
	// "net/http"
	// "net/url"
	"bufio"
	"os"
	"io"
	// "io/ioutil"
	// "sync"
	"time"
	// "strings"
	// "bytes"
	"github.com/sscsdl/proxy/pipe"
	"github.com/sscsdl/proxy/proxy"
)

var (
	// quit = make(chan bool, 1)
	isDebug = flag.Bool("debug", false, "debug")
	model = flag.Int("model", 0, "0: normal proxy\n1: master-passive\n2: slave-aActive")
	connNum = make(chan int, 100)
	
	// slaveActive
	keepConnNum = flag.Int("k", 10, "keep number of conncet")
	block       = flag.Bool("block", true, "block file")
	exit        = make(chan int, 1)
	ip 			= flag.String("ip", "", "master proxy")
	proxyServer proxy.Proxy
)

func debug(args ...interface{}) {
	if *isDebug {
		fmt.Println(args...)
	}
}

func main() {
	// 解析命令行参数
	flag.Parse()

	switch *model {
	case 0:
		// normal proxy
		normal()
	case 1:
		// master-passive
		masterPassive()
	case 2:
		// slave-aActive
		slaveActive()
	}
}

func normal() {
	if *isDebug {
		go proxyStat()
	}

	proxyServer := proxy.NewPorxy("9010")
	proxyServer.Debug = *isDebug
	
	// 设置禁止访问域名
	if *block {
		proxyServer.SetBlockList(loadBlockFile())
	}
	
	proxyServer.ListenAndAccept(proxyServer.HandleConnection)
}

func masterPassive() {
	if *isDebug {
		go stat()
	}

	proxyServer := proxy.NewPorxy("9010")
	proxyServer.Debug = *isDebug
	proxyServer.Listen()

	pipeServer := pipe.NewPipe()
	pipeServer.Debug = *isDebug
	pipeServer.Listen()
	
	for {
		pipeConn := pipeServer.Accept()
		reqConn := proxyServer.Accept()
		debug(reqConn.RemoteAddr(), "<=>", pipeConn.RemoteAddr())
		go func() {
			defer func() {
				<- connNum
			}()
			connNum <- 1
			proxyServer.Docking(reqConn, pipeConn)
		}()
	}
}

func slaveActive() {
	if *ip == "" {
		fmt.Println("ip cannot be empty!")
		return
	}

	if *isDebug {
		go stat()
	}

	pipeServer := pipe.NewPipe()
	pipeServer.Debug = *isDebug

	proxyServer := proxy.NewPorxy("9010")
	proxyServer.Debug = *isDebug
	// 设置禁止访问域名
	if *block {
		proxyServer.SetBlockList(loadBlockFile())
	}
	// 监听连接第一次传输数据事件
	proxyServer.ListenEvent(proxy.EventFirstdata, func(conn net.Conn) {
		// 新建一条新连接
		// pipeServer.CreateEmptyConn()
		pipeConn := pipeServer.Connect(*ip)
		handleReq(proxyServer, pipeConn)
	})

	// 设置空闲链接数
	for index := 0; index < *keepConnNum; index++ {
		pipeConn := pipeServer.Connect(*ip)
		go handleReq(proxyServer, pipeConn)
	}

	<-exit
}

func handleReq(proxyServer *proxy.Proxy, pipeConn net.Conn) {
	defer func() {
		pipeConn.Close()
		<-connNum
	}()
	connNum <- 1
	
	proxyServer.HandleConnection(pipeConn)
}

func stat() {
	for {
		debug("stat connNum:", len(connNum))
		time.Sleep(10 * time.Second)
	}
}

func proxyStat() {
	for {
		debug("stat connNum:", proxyServer.ConnNum())
		time.Sleep(10 * time.Second)
	}
}

func loadBlockFile() map[string]bool {
	var list = make(map[string]bool)
	fi, err := os.Open("./blockdomain")
	if err != nil {
		debug("empty blockdomain")
		return list
	}
	defer fi.Close()

	br := bufio.NewReader(fi)
	for {
		a, _, c := br.ReadLine()
		if c == io.EOF {
			break
		}
		list[string(a)] = true
	}
	return list
}