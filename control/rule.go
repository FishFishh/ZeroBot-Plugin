// Package control 控制插件的启用与优先级等
package control

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension"
	"github.com/wdvxdr1123/ZeroBot/message"

	"github.com/FloatTech/ZeroBot-Plugin/utils/sql"
)

const ALL int64 = 0

var (
	db = &sql.Sqlite{DBPath: "data/control/plugins.db"}
	// managers 每个插件对应的管理
	managers = map[string]*Control{}
	mu       = sync.RWMutex{}
	hasinit  bool
)

// Control is to control the plugins.
type Control struct {
	sync.RWMutex
	service string
	options Options
}

// newctrl returns Manager with settings.
func newctrl(service string, o *Options) *Control {
	m := &Control{service: service,
		options: func() Options {
			if o == nil {
				return Options{}
			}
			return *o
		}(),
	}
	mu.Lock()
	managers[service] = m
	mu.Unlock()
	err := db.Create(service, &grpcfg{})
	if err != nil {
		panic(err)
	}
	return m
}

// Enable enables a group to pass the Manager.
// groupID == 0 (ALL) will operate on all grps.
func (m *Control) Enable(groupID int64) {
	m.Lock()
	err := db.Insert(m.service, &grpcfg{groupID, 0})
	if err != nil {
		logrus.Errorf("[control] %v", err)
	}
	m.Unlock()
}

// Disable disables a group to pass the Manager.
// groupID == 0 (ALL) will operate on all grps.
func (m *Control) Disable(groupID int64) {
	m.Lock()
	err := db.Insert(m.service, &grpcfg{groupID, 1})
	if err != nil {
		logrus.Errorf("[control] %v", err)
	}
	m.Unlock()
}

// IsEnabledIn 开启群
func (m *Control) IsEnabledIn(gid int64) bool {
	m.RLock()
	var c grpcfg
	err := db.Find(m.service, &c, "WHERE gid = "+strconv.FormatInt(ALL, 10))
	if err == nil {
		logrus.Debugf("[control] plugin %s of all : %d", m.service, c.GroupID, c.Disable)
		if c.Disable != 0 {
			m.RUnlock()
			return false
		}
	}
	err = db.Find(m.service, &c, "WHERE gid = "+strconv.FormatInt(gid, 10))
	if err == nil {
		m.RUnlock()
		logrus.Debugf("[control] plugin %s of grp %d : %d", m.service, c.GroupID, c.Disable)
		return c.Disable == 0
	}
	logrus.Errorf("[control] %v", err)
	m.RUnlock()
	if m.options.DisableOnDefault {
		m.Disable(gid)
	} else {
		m.Enable(gid)
	}
	return !m.options.DisableOnDefault
}

// Handler 返回 预处理器
func (m *Control) Handler() zero.Rule {
	return func(ctx *zero.Ctx) bool {
		ctx.State["manager"] = m
		grp := ctx.Event.GroupID
		if grp == 0 {
			// 个人用户
			grp = -ctx.Event.UserID
		}
		return m.IsEnabledIn(grp)
	}
}

// Lookup returns a Manager by the service name, if
// not exist, it will returns nil.
func Lookup(service string) (*Control, bool) {
	mu.RLock()
	m, ok := managers[service]
	mu.RUnlock()
	return m, ok
}

// ForEach iterates through managers.
func ForEach(iterator func(key string, manager *Control) bool) {
	mu.RLock()
	m := copyMap(managers)
	mu.RUnlock()
	for k, v := range m {
		if !iterator(k, v) {
			return
		}
	}
}

func copyMap(m map[string]*Control) map[string]*Control {
	ret := make(map[string]*Control, len(m))
	for k, v := range m {
		ret[k] = v
	}
	return ret
}

func init() {
	if !hasinit {
		mu.Lock()
		if !hasinit {
			err := os.MkdirAll("data/control", 0755)
			if err != nil {
				panic(err)
			} else {
				hasinit = true
				zero.OnCommandGroup([]string{
					"启用", "enable", "禁用", "disable",
					"全局启用", "enableall", "全局禁用", "disableall",
				}, func(ctx *zero.Ctx) bool {
					if zero.OnlyGroup(ctx) {
						return zero.AdminPermission(ctx)
					}
					return zero.OnlyToMe(ctx)
				}).
					Handle(func(ctx *zero.Ctx) {
						model := extension.CommandModel{}
						_ = ctx.Parse(&model)
						service, ok := Lookup(model.Args)
						if !ok {
							ctx.SendChain(message.Text("没有找到指定服务!"))
						}
						grp := ctx.Event.GroupID
						if grp == 0 {
							// 个人用户
							grp = -ctx.Event.UserID
						}
						if strings.Contains(model.Command, "全局") || strings.Contains(model.Command, "all") {
							grp = 0
						}
						if strings.Contains(model.Command, "启用") || strings.Contains(model.Command, "enable") {
							service.Enable(grp)
							ctx.SendChain(message.Text("已启用服务: " + model.Args))
						} else {
							service.Disable(grp)
							ctx.SendChain(message.Text("已禁用服务: " + model.Args))
						}
					})

				zero.OnCommandGroup([]string{"用法", "usage"}, zero.AdminPermission, zero.OnlyGroup).
					Handle(func(ctx *zero.Ctx) {
						model := extension.CommandModel{}
						_ = ctx.Parse(&model)
						service, ok := Lookup(model.Args)
						if !ok {
							ctx.SendChain(message.Text("没有找到指定服务!"))
						}
						if service.options.Help != "" {
							ctx.SendChain(message.Text(service.options.Help))
						} else {
							ctx.SendChain(message.Text("该服务无帮助!"))
						}
					})

				zero.OnCommandGroup([]string{"服务列表", "service_list"}, zero.AdminPermission, zero.OnlyGroup).
					Handle(func(ctx *zero.Ctx) {
						msg := `---服务列表---`
						i := 0
						ForEach(func(key string, manager *Control) bool {
							i++
							msg += "\n" + strconv.Itoa(i) + `: `
							if manager.IsEnabledIn(ctx.Event.GroupID) {
								msg += "●" + key
							} else {
								msg += "○" + key
							}
							return true
						})
						ctx.SendChain(message.Text(msg))
					})
			}
		}
		mu.Unlock()
	}
}
