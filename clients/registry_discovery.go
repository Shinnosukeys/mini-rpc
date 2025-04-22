package clients

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RegistryDiscovery struct {
	*MultiServersDiscovery
	registry   string               // 该字段存储的是注册中心的网络地址
	timeout    time.Duration        // RegistryDiscovery 使用这个时间间隔来判断距离上次更新服务器列表后是否已经过了足够长的时间，从而决定是否需要再次从注册中心刷新服务器列表
	lastUpdate map[string]time.Time // 该字段记录了 RegistryDiscovery 上次更新服务器列表的时间。它与 timeout 配合使用，用于判断当前的服务器列表是否已经过期
	mu         sync.Mutex
}

const defaultUpdateTimeout = time.Second * 10

// NewRegistryDiscovery 创建注册中心发现实例
func NewRegistryDiscovery(registryAddr string, timeout time.Duration) *RegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	return &RegistryDiscovery{
		MultiServersDiscovery: NewMultiServerDiscovery(make(map[string][]string)), // 初始化空服务
		registry:              registryAddr,
		timeout:               timeout,
		lastUpdate:            make(map[string]time.Time), // 按服务名独立记录更新时间
	}
}

// Refresh 按服务名从注册中心刷新地址（实现 Discovery 接口）
func (d *RegistryDiscovery) Refresh(serviceName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 检查是否需要刷新（按服务名独立判断）
	if last, exists := d.lastUpdate[serviceName]; exists && last.Add(d.timeout).After(time.Now()) {
		return nil // 未超时，无需刷新
	}

	log.Printf("rpc registry: refresh service %s from %s", serviceName, d.registry)
	reqURL := d.registry + "?service=" + serviceName // 携带服务名参数

	resp, err := http.Get(reqURL)
	if err != nil {
		log.Printf("rpc registry refresh failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	// 解析注册中心返回的地址列表
	servers := strings.Split(resp.Header.Get("X-Mini-rpc-Servers"), ",")
	cleaned := make([]string, 0, len(servers))
	for _, addr := range servers {
		if addr = strings.TrimSpace(addr); addr != "" {
			cleaned = append(cleaned, addr)
		}
	}

	// 更新 MultiServersDiscovery 中的服务地址（线程安全）
	if err := d.MultiServersDiscovery.Update(serviceName, cleaned); err != nil {
		return err
	}

	// 记录更新时间（按服务名）
	d.lastUpdate[serviceName] = time.Now()
	return nil
}

// Get 按服务名和负载模式获取地址（调整参数顺序：服务名优先）
func (d *RegistryDiscovery) Get(serviceName string, mode SelectMode) (string, error) {
	if err := d.Refresh(serviceName); err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(serviceName, mode) // 传递服务名到负载均衡
}

// GetAll 按服务名获取所有地址
func (d *RegistryDiscovery) GetAll(serviceName string) ([]string, error) {
	if err := d.Refresh(serviceName); err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll(serviceName)
}
