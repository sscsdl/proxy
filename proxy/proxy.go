package proxy

import (
	"fmt"
	"net"
	// "net/http"
	"net/url"
	// "bufio"
	// "os"
	"io"
	// "io/ioutil"
	"sync"
	"time"
	"strings"
	// "bytes"
	// "connpipe"
)

const (
	// EventFirstdata event is triggered when data is received for the first time
	EventFirstdata = iota
)

// NewPorxy create a new proxy
func NewPorxy(port string) *Proxy {
	return &Proxy{
		port: port,
		connNum: make(chan int, 100),
		eventCallback: make(map[int] Callback, 1),
		event: make(chan Event, 100),
	}
}

// Callback type
type Callback func (conn net.Conn)

// Event type
type Event struct {
	no int
	conn net.Conn
}

// Proxy struct
type Proxy struct {
	Debug bool
	port string
	connNum chan int
	ln net.Listener
	// 事件
	event chan Event
	eventCallback map[int] Callback
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
				<- p.connNum
				conn.Close()
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
	p.ln, err = net.Listen("tcp", ":" + p.port)
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
		p.debug("close back connect", conn.LocalAddr())
	}()
	
	reqBody, err := p.getRequest(conn)
	// 产生数据第一次传输事件
	p.event <- Event{no: EventFirstdata, conn: conn}
	if err != nil {
		p.debug("getRequest nil", err)
		return
	}
	// 解析请求
	req = parseReq(string(reqBody))
	// p.debug(req)
	addrArr := strings.Split(conn.LocalAddr().String(), ":")
	req.proxyConnPort = addrArr[1]

	defer func() {
		p.debug(req.proxyConnPort, "close back connect", req.hostname + ":" + req.port)
	}()

	// 拦截请求
	if block := p.blockReq(req.hostname); block {
		conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\n"))
		p.debug(req.proxyConnPort, "block:", req.hostname)
		return
	}
	p.debug("request:", req.method, req.url)
	
	if "CONNECT" == req.method {
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
	dstConn, err := net.Dial("tcp", req.hostname + ":" + req.port)
	if err != nil {
		p.debug(req.proxyConnPort, "connect http err:", err)
		return
	}
	defer dstConn.Close()
	_, err = dstConn.Write(reqBody)
	if err != nil {
		p.debug(req.proxyConnPort, "sent to http err:", err)
		return
	}
	p.Docking(srcConn, dstConn)
}

// handle https request
func (p *Proxy) httpsHandle(req *Req, srcConn net.Conn) {
	dstConn, err := net.Dial("tcp", req.url)
	if err != nil {
		p.debug(req.proxyConnPort, "connect https err:", err)
		return
	}
	defer dstConn.Close()
	_, err = srcConn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	if err != nil {
		p.debug(req.proxyConnPort, "sent to https err:", err)
		return
	}
	p.Docking(srcConn, dstConn)
}

// Docking transfer connect
func (p *Proxy) Docking(src net.Conn, dst net.Conn) {
	defer func() {
		p.debug("close docking", src.RemoteAddr())
		src.Close()
		dst.Close()
		// <- connNum
	}()
	var (
		wg sync.WaitGroup
		closeFlag bool = false
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
		from.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := from.Read(buf)
		// p.debug(way, "copy", n)
		if n > 0 {
			_, err := sentto.Write(buf[:n])
			if err != nil {
				p.debug(way, "Write err", err)
				break
			}
		}
		if err == io.EOF {
			p.debug(way, "io.EOF")
			break
		} else if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
                continue
            }
			// *closeFlag = true
			p.debug(way, "err", err)
			break
		}
	}
	sentto.SetReadDeadline(time.Now().Add(5 * time.Second))
	*closeFlag = true
	return 
}

func (p *Proxy) eventHandleServer() {
	var event Event
	for {
		event = <- p.event
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
		result = []byte{}
		firstRead bool = true
	)
	buf := make([]byte, 2048)
	for {
		if firstRead {
			from.SetReadDeadline(time.Now().Add(5 * time.Second))
			firstRead = false
		} else {
			from.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
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

// Req type
type Req struct {
	method string
	hostname string
	port string
	url string
	body string
	proxyConnPort string
}

func parseReq(result string) (req *Req) {
	req = &Req{}
	lineArr := strings.Split(result, "\r\n")
	// this.debug("parseReq:", lineArr[0])
	
	firstLineArr := strings.Split(lineArr[0], " ")
	req.method = firstLineArr[0]
	req.url = firstLineArr[1]
	req.body = result
	// this.debug(req.method, req.url)
	if "CONNECT" == req.method {
		httpsURL := strings.Split(firstLineArr[1], ":")
		req.hostname, req.port = httpsURL[0], httpsURL[1]
		return
	}
	urlObj, err := url.Parse(req.url)
	if err != nil {
		panic("err")
	}
	req.hostname, req.port = urlObj.Hostname(), urlObj.Port()
	if "" == req.port {
		req.port = "80"
	}
	return
}