package pipe

import (
	"fmt"
	"net"
	// "net/http"
	// "net/url"
	// "bufio"
	// "os"
	// "io"
	// "sync"
	"time"
)

const (
	// EventFirstdata : When the request was first received
	EventFirstdata = iota
)

// Callback type
type Callback func (conn net.Conn)

// Event type
type Event struct {
	no int
	conn net.Conn
}

// NewPipe create a new pipe
func NewPipe() *Pipe {
	return &Pipe{
		port: "9012",
		needNewConn: make(chan bool, 100),
		// connPool: make(chan net.Conn, 100),
		connNum: make(chan int, 100),
		// Ready: make(chan bool, 3),
		eventCallback: make(map[int] Callback, 1),
		event: make(chan Event, 100),
	}
}

// Pipe struct
type Pipe struct {
	// debug
	Debug bool
	// 监听端口
	port string
	// 连接IP
	connIP string
	// 管道状态 false：不可用 true：可用
	// status bool
	// 管道就绪通知
	// Ready chan bool
	// 可用连接池
	connPool chan net.Conn
	// 当前连接数
	connNum chan int
	// 新建空连接
	needNewConn chan bool
	// 预留空连接数
	emptyConnNum int
	// callback
	callback Callback
	// Listener
	ln net.Listener
	// 事件
	event chan Event
	eventCallback map[int] Callback
}

// Listen connect
func (p *Pipe) Listen() {
	p.debug("Listen:", p.port)
	var err error
	p.ln, err = net.Listen("tcp", ":" + p.port)
	if err != nil {
		panic(err)
	}
}

// Accept get connect
func (p *Pipe) Accept() net.Conn {
	conn, err := p.ln.Accept()
	if err != nil {
		panic(err)
	}
	p.debug("Accept:", conn.RemoteAddr())
	return conn
}

func (p *Pipe) callCallback(conn net.Conn) {
	defer func() {
		p.debug("callback end")
		<- p.connNum
		conn.Close()
	}()
	p.connNum <- 1
	p.callback(conn)
}

// Close pipe
func (p *Pipe) Close() {
	p.ln.Close()
}

// Connect pipe server
func (p *Pipe) Connect(masterProxyAddr string) net.Conn {
	p.connIP = masterProxyAddr
	var (
		conn net.Conn
		err error
	)
	for {
		conn, err = net.DialTimeout("tcp", p.connIP + ":" + p.port, 10 * time.Second)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	// emptyConn <- true
	p.debug("pipe connected")
	// p.callCallback(conn)
	return conn
}

// CurrentConnNum get number of current connect
func (p *Pipe) CurrentConnNum() int {
	return len(p.connNum)
}

func (p *Pipe) eventHandleServer() {
	var event Event
	for {
		event = <- p.event
		// if callback, ok := p.eventCallback[event.no]; ok {
		go p.eventCallback[event.no](event.conn)
		// }
	}
}

// ListenEvent event
func (p *Pipe) ListenEvent(event int, callback Callback) error {
	p.eventCallback[event] = callback
	return nil
}

func (p *Pipe) debug(args ...interface{}) {
	if p.Debug {
		fmt.Print("pipe ")
		fmt.Println(args...)
	}
}