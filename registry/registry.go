package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Registry is a simple register center, provide following functions.
// add a server and receive heartbeat to keep it alive.
// returns all alive servers and delete dead servers sync simultaneously.
type Registry struct {
	timeout time.Duration
	mu      sync.Mutex // protect following
	//servers map[string]*ServerItem

	// 支持服务名称的注册与发现
	services map[string]map[string]*ServerItem // key: 服务名称，value: 该服务的地址集合
}

type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath    = "/_minirpc_/registry"
	defaultTimeout = time.Minute * 5
)

// New create a registry instance with timeout setting
func New(timeout time.Duration) *Registry {
	return &Registry{
		services: make(map[string]map[string]*ServerItem), // 初始化按服务分组的地址表
		timeout:  timeout,
	}
}

var DefaultRegister = New(defaultTimeout)

// putServer 按服务名称注册/更新服务器地址（新增服务名称参数）
func (r *Registry) putServer(serviceName, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 初始化该服务的地址表（若不存在）
	if _, exists := r.services[serviceName]; !exists {
		r.services[serviceName] = make(map[string]*ServerItem)
	}

	item := r.services[serviceName][addr]
	if item == nil {
		// 首次注册：记录地址和初始时间
		r.services[serviceName][addr] = &ServerItem{Addr: addr, start: time.Now()}
	} else {
		// 心跳更新：刷新存活时间
		item.start = time.Now()
	}
}

func (r *Registry) aliveServers(serviceName string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	addrs, exists := r.services[serviceName]
	if !exists {
		return nil
	}

	var alive []string
	for addr, s := range addrs {
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(addrs, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

// Runs at /_minirpc_/registry
// ServeHTTP 处理注册中心HTTP请求（支持服务名称参数）
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		// 获取请求中的服务名称（通过查询参数或请求头，此处用查询参数示例）
		serviceName := req.URL.Query().Get("service")
		if serviceName == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		servers := r.aliveServers(serviceName)
		w.Header().Set("X-Mini-rpc-Servers", strings.Join(servers, ","))

	case "POST":
		// 接收服务名称和地址（通过请求头传递）
		serviceName := req.Header.Get("X-Mini-rpc-Service")
		addr := req.Header.Get("X-Mini-rpc-Server")
		if serviceName == "" || addr == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.putServer(serviceName, addr) // 按服务名称注册地址

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleHTTP registers an HTTP handler for GeeRegistry messages on registryPath
func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path:", registryPath)
}

func HandleHTTP() {
	DefaultRegister.HandleHTTP(defaultPath)
}

// Heartbeat send a heartbeat message every once in a while
// it's a helper function for a server to register or send heartbeat
// Heartbeat 服务器心跳函数（新增服务名称参数）
func Heartbeat(registryURL, serviceName, addr string, duration time.Duration) {
	if duration == 0 {
		duration = defaultTimeout - time.Minute // 预留1分钟缓冲时间
	}
	var err error
	// 首次注册
	if err = sendHeartbeat(registryURL, serviceName, addr); err != nil {
		log.Printf("initial heartbeat failed: %v", err)
	}

	// 定时发送心跳
	go func() {
		ticker := time.NewTicker(duration)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				err = sendHeartbeat(registryURL, serviceName, addr)
				if err != nil {
					log.Printf("heartbeat failed: %v", err)
				}
			}
		}
	}()
}

func sendHeartbeat(registry, serviceName, addr string) error {
	log.Println(addr, "send heart beat to registry", registry)
	httpClient := &http.Client{}
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-Mini-rpc-Service", serviceName) // 服务名称头
	req.Header.Set("X-Mini-rpc-Server", addr)         // 地址头
	if _, err := httpClient.Do(req); err != nil {
		log.Println("rpc server: heart beat err:", err)
		return err
	}
	return nil
}
