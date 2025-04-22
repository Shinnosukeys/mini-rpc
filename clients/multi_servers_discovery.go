package clients

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

type Discovery interface {
	Refresh(serviceName string) error                        // 按服务名刷新地址（可选，非注册中心场景可空实现）
	Update(serviceName string, servers []string) error       // 按服务名更新地址
	Get(serviceName string, mode SelectMode) (string, error) // 按服务名和负载模式获取地址
	GetAll(serviceName string) ([]string, error)             // 按服务名获取所有地址
}

var _ Discovery = (*MultiServersDiscovery)(nil)

// MultiServersDiscovery is a discovery for multi servers without a registry center
// user provides the server addresses explicitly instead
type MultiServersDiscovery struct {
	r        *rand.Rand          // 随机数生成器
	mu       sync.RWMutex        // 保护并发访问
	services map[string][]string // 服务名称 -> 地址列表（核心改造点）
	index    map[string]int      // 轮询算法的索引（按服务名隔离，避免不同服务轮询互相影响）
}

func NewMultiServerDiscovery(services map[string][]string) *MultiServersDiscovery {
	d := &MultiServersDiscovery{
		services: make(map[string][]string),
		index:    make(map[string]int),
		r:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// 初始化各服务的地址和轮询索引
	for serviceName, servers := range services {
		d.services[serviceName] = servers
		d.index[serviceName] = d.r.Intn(math.MaxInt32 - 1) // 随机初始轮询位置
	}
	return d
}

// Refresh 非注册中心场景无需刷新（空实现）
func (m *MultiServersDiscovery) Refresh(serviceName string) error {
	return nil
}

// Update 按服务名更新地址列表（支持动态添加/修改服务）
func (m *MultiServersDiscovery) Update(serviceName string, servers []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[serviceName] = servers
	// 重置轮询索引（避免旧索引指向已删除的地址）
	m.index[serviceName] = m.r.Intn(math.MaxInt32 - 1)
	return nil
}

// Get 按服务名和负载模式获取单个地址
func (m *MultiServersDiscovery) Get(serviceName string, mode SelectMode) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	servers, exists := m.services[serviceName]
	if !exists || len(servers) == 0 {
		return "", errors.New("discovery: service not found or no available servers")
	}
	n := len(servers)
	switch mode {
	case RandomSelect:
		return servers[m.r.Intn(n)], nil
	case RoundRobinSelect:
		idx := m.index[serviceName] % n
		m.index[serviceName] = (idx + 1) % n // 轮询索引独立递增（按服务名隔离）
		return servers[idx], nil
	default:
		return "", errors.New("discovery: unsupported select mode")
	}
}

// NewMultiServerDiscovery creates a MultiServersDiscovery instance
// GetAll 按服务名获取所有地址（返回副本，避免并发修改）
func (m *MultiServersDiscovery) GetAll(serviceName string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	servers, exists := m.services[serviceName]
	if !exists {
		return nil, errors.New("discovery: service not found")
	}
	// 返回地址副本，防止外部修改影响内部状态
	copied := make([]string, len(servers))
	copy(copied, servers)
	return copied, nil
}
