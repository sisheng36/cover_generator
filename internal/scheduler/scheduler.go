package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Manager struct {
	mu      sync.RWMutex
	cron    *cron.Cron
	entryID cron.EntryID
	running bool
	loc     *time.Location
}

func New() *Manager {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return &Manager{loc: loc}
}

func (m *Manager) Start(enabled bool, expr string, job func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopLocked()
	if !enabled {
		log.Printf("定时任务未启用")
		return
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	c := cron.New(cron.WithLocation(m.loc), cron.WithParser(parser))
	id, err := c.AddFunc(expr, job)
	if err != nil {
		log.Printf("定时任务启动失败: %v", err)
		return
	}

	c.Start()
	m.cron = c
	m.entryID = id
	m.running = true
	log.Printf("定时任务已启动: cron=%s", expr)
}

func (m *Manager) Restart(enabled bool, expr string, job func()) {
	m.Start(enabled, expr, job)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.cron != nil {
		_ = m.cron.Stop()
	}
	m.cron = nil
	m.entryID = 0
	m.running = false
}

func (m *Manager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *Manager) NextRun() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running || m.cron == nil {
		return ""
	}
	entry := m.cron.Entry(m.entryID)
	if entry.ID == 0 || entry.Next.IsZero() {
		return ""
	}
	return entry.Next.In(m.loc).Format("2006-01-02 15:04:05")
}

func (m *Manager) String() string {
	return fmt.Sprintf("running=%t next=%s", m.Running(), m.NextRun())
}
