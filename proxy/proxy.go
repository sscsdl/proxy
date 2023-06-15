package proxy

import (
	"fmt"
	"net"
	// "net/http"
	"net/url"
	// "bufio"
	// "os"
	"bytes"
	"io"
	"strings"
	// "io/ioutil"
	"sync"
	"time"
	// "connpipe"
)

const (
	// EventFirstdata event is triggered when data is received for the first time
	EventFirstdata = iota
)

// NewPorxy create a new proxy
func NewPorxy(port string) *Proxy {
	return &Proxy{
		port:          port,
		connNum:       make(chan int, 100),
		eventCallback: make(map[int]Callback, 1),
		event:         make(chan Event, 100),
	}
}

// Callback type
type Callback func(conn net.Conn)

// Event type
type Event struct {
	no   int
	conn net.Conn
}

// Proxy struct
type Proxy struct {
	Debug   bool
	port    string
	connNum chan int
	ln      net.Listener
	// 事件
	event         chan Event
	eventCallback map[int]Callback
	blockHostList map[string]bool
}

// ListenAndAccept listen and accept connect
func (p *Proxy) ListenAndAccept(callback Callback) {
	p.Listen()
	for {
		conn := p.Accept()
		go func() {
			defer func() {
				p.debug("callback end")
				<-p.connNum
				_ = conn.Close()
			}()
			p.connNum <- 1
			callback(conn)
		}()
	}
}

// Listen connect
func (p *Proxy) Listen() {
	p.debug("Listen:", p.port)
	var err error
	p.ln, err = net.Listen("tcp", ":"+p.port)
	if err != nil {
		panic(err)
	}
}

// Accept connect
func (p *Proxy) Accept() net.Conn {
	conn, err := p.ln.Accept()
	if err != nil {
		panic(err)
	}
	p.debug("Accept:", conn.RemoteAddr())
	return conn
}

// Close connect
func (p *Proxy) Close() {
	p.ln.Close()
}

// HandleConnection handle connect
func (p *Proxy) HandleConnection(conn net.Conn) {
	var (
		req *Req
	)

	defer func() {
		p.debugConn(conn, "close")
		_ = conn.Close()
	}()

	reqBody, err := p.getRequest(conn)
	// 产生数据第一次传输事件
	p.event <- Event{no: EventFirstdata, conn: conn}
	if err != nil {
		p.debugConn(conn, "getRequest nil", err)
		return
	}
	// 解析请求
	req = parseReq(reqBody)

	// 拦截请求
	if block := p.blockReq(req.hostname); block {
		conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
		p.debugConn(conn, req.proxyConnPort, "block:", req.hostname)
		return
	}
	p.debugConn(conn, "request:", req.method, req.url)

	if "CONNECT" == req.method || req.https {
		p.httpsHandle(req, conn)
	} else {
		p.httpHandle(req, conn, reqBody)
	}
	return
}

// blockReq check if the domain is block
func (p *Proxy) blockReq(hostname string) bool {
	if _, ok := p.blockHostList[hostname]; ok {
		return true
	}
	return false
}

// SetBlockList set block domain
func (p *Proxy) SetBlockList(blockHostList map[string]bool) {
	p.blockHostList = blockHostList
}

// handle http request
func (p *Proxy) httpHandle(req *Req, srcConn net.Conn, reqBody []byte) {
	dstConn, err := net.Dial("tcp", req.hostname+":"+req.port)
	if err != nil {
		p.debugConn(srcConn, "connect http err:", err)
		return
	}
	defer dstConn.Close()
	_, err = dstConn.Write(reqBody)
	if err != nil {
		p.debugConn(srcConn, "sent to http err:", err)
		return
	}
	p.Docking(srcConn, dstConn)
}

// handle https request
func (p *Proxy) httpsHandle(req *Req, srcConn net.Conn) {
	dstConn, err := net.Dial("tcp", req.url)
	if err != nil {
		p.debugConn(srcConn, "connect https err:", err)
		return
	}
	defer dstConn.Close()
	_, err = srcConn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	if err != nil {
		p.debugConn(srcConn, "sent to https err:", err)
		return
	}
	p.Docking(srcConn, dstConn)
}

// Docking transfer connect
func (p *Proxy) Docking(src net.Conn, dst net.Conn) {
	defer func() {
		p.debugConn(src, "close docking")
		// <- connNum
	}()
	var (
		wg        sync.WaitGroup
		closeFlag = false
	)
	wg.Add(2)
	go p.transfer(src, dst, &wg, &closeFlag, "send")
	go p.transfer(dst, src, &wg, &closeFlag, "back")
	wg.Wait()
}

func (p *Proxy) transfer(from net.Conn, sentto net.Conn, wg *sync.WaitGroup, closeFlag *bool, way string) {
	defer wg.Done()

	buf := make([]byte, 4096)
	for {
		if *closeFlag {
			break
		}
		_ = from.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := from.Read(buf)
		if err == io.EOF {
			if way == "send" {
				p.debugConn(from, "=> io.EOF")
			} else {
				p.debugConn(sentto, "<= io.EOF")
			}
			break
		} else if err != nil {
			if nerr, ok := err.(net.Error); ok && (nerr.Timeout() || nerr.Temporary()) {
				if way == "send" {
					p.debugConn(from, "=> net err", nerr)
				} else {
					p.debugConn(sentto, "<= net err", nerr)
				}
				continue
			}
			if way == "send" {
				p.debugConn(from, "=> err", err)
			} else {
				p.debugConn(sentto, "<= err", err)
			}
			break
		}
		if n > 0 {
			// fmt.Println(string(buf[:n]))
			_, err := sentto.Write(buf[:n])
			if err != nil {
				if way == "send" {
					p.debugConn(from, "=> write err")
				} else {
					p.debugConn(sentto, "<= write err")
				}
				break
			}
		}
		if way == "send" {
			p.debugConn(from, "=> n==0")
		} else {
			p.debugConn(sentto, "<= n==0")
		}
	}
	_ = sentto.SetReadDeadline(time.Now().Add(5 * time.Second))
	*closeFlag = true
	return
}

func (p *Proxy) eventHandleServer() {
	var event Event
	for {
		event = <-p.event
		// if callback, ok := p.eventCallback[event.no]; ok {
		go p.eventCallback[event.no](event.conn)
		// }
	}
}

// ListenEvent listen event
func (p *Proxy) ListenEvent(event int, callback Callback) error {
	// 启动事件处理服务
	if len(p.eventCallback) == 0 {
		go p.eventHandleServer()
	}
	p.eventCallback[event] = callback
	return nil
}

func (p *Proxy) getRequest(from net.Conn) ([]byte, error) {
	var (
		result    []byte
		firstRead = true
	)
	buf := make([]byte, 2048)
	for {
		if firstRead {
			_ = from.SetReadDeadline(time.Now().Add(5 * time.Second))
			firstRead = false
		} else {
			_ = from.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		}
		n, err := from.Read(buf)
		if err == io.EOF {
			p.debug("getRequest end", len(result))
			// return result
			if len(result) > 0 {
				break
			}
			return []byte{}, err
			// break
		} else if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				if len(result) > 0 {
					break
				}
				continue
			}
			p.debug("getRequest err:", err)
			return []byte{}, err
		} else if n <= 0 {
			p.debug("getRequest end:", 0)
			break
		}
		// debug("getRequest", n)
		result = append(result, buf[:n]...)
	}
	return result, nil
}

// ConnNum number of connect, Only if use ListenAndAccept function
func (p *Proxy) ConnNum() int {
	return len(p.connNum)
}

func (p *Proxy) debug(args ...interface{}) {
	if p.Debug {
		fmt.Print("proxy ")
		fmt.Println(args...)
	}
}
func (p *Proxy) debugConn(conn net.Conn, args ...interface{}) {
	if p.Debug {
		localAddr := conn.LocalAddr().String()
		index := strings.LastIndex(localAddr, ":")
		localPort := localAddr[index+1:]
		fmt.Printf("proxy[%s] ", localPort)
		fmt.Println(args...)
	}
}

// Req type
type Req struct {
	method        string
	hostname      string
	port          string
	url           string
	body          []byte
	proxyConnPort string
	https         bool
}

func parseReq(result []byte) (req *Req) {
	req = &Req{}
	index := bytes.Index(result, []byte{'\n'})
	if index == -1 {
		fmt.Println(string(result))
		panic("parseReq err 1")
	}
	firstLine := result[:index]
	req.body = result
	var ok bool
	req.method, req.url, ok = parseRequestLine(firstLine)
	if !ok {
		fmt.Println(string(result))
		fmt.Printf("%x", result)
		panic("parseReq err 2")
	}
	if "CONNECT" == req.method {
		index = bytes.Index(firstLine, []byte{':'})
		req.hostname, req.port = string(firstLine[:index]), string(firstLine[index+1:])
		req.https = true
	} else {
		urlObj, err := url.Parse(req.url)
		if err != nil {
			fmt.Println(string(firstLine))
			fmt.Println(req.url)
			panic(err)
		}
		if urlObj.Scheme == "https" {
			req.https = true
		}
		req.hostname, req.port = urlObj.Hostname(), urlObj.Port()
	}
	if "" == req.port {
		req.port = "80"
	}
	return
}

// parseRequestLine parses "GET /foo HTTP/1.1" into its three parts.
func parseRequestLine(line []byte) (method, requestURI string, ok bool) {
	s1 := bytes.Index(line, []byte{' '})
	s2 := bytes.Index(line[s1+1:], []byte{' '})
	if s1 < 0 || s2 < 0 {
		return
	}
	s2 += s1 + 1
	return string(line[:s1]), string(line[s1+1 : s2]), true
}
