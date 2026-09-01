package session

// Domain 的 actor 化实现（无锁化：注册表 + 视图指针 V 由单一 goroutine
// 显式持有，channel 命令；取代原 sync.RWMutex）。
//
// 共享面 G=(V, registry, …)：V 与会话注册表是唯一跨会话共享状态，收进
// actor 后不再有任何共享 mutex。每会话数据（View/Chat/Queue）仍在单元
// 内部串行（下一阶段 per-session actor）。

type domainCmdKind int

const (
	domainCmdRegister domainCmdKind = iota
	domainCmdRemove
	domainCmdUnit
	domainCmdUnitIDs
	domainCmdLive
	domainCmdSetActive
	domainCmdActiveID
)

type domainReply struct {
	unit   *SessionUnit
	ids    []string
	live   int
	active string
}

type domainCmd struct {
	kind  domainCmdKind
	unit  *SessionUnit
	sid   string
	reply chan domainReply
}

// Domain 是会话域：actor 持有 units 注册表与 activeID（V 指针）。
// 方法经 channel 命令与单 goroutine 交互，无互斥锁。
type Domain struct {
	cmds   chan domainCmd
	stopCh chan struct{}
	done   chan struct{}
}

// NewDomain 启动会话域 actor（单 goroutine 持有注册表与 V）。
func NewDomain() *Domain {
	domain := &Domain{
		cmds:   make(chan domainCmd, 64),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go domain.loop()
	return domain
}

// loop 是注册表与 V 的唯一所有者。
func (domain *Domain) loop() {
	defer close(domain.done)
	units := make(map[string]*SessionUnit)
	activeID := ""
	for {
		select {
		case cmd := <-domain.cmds:
			switch cmd.kind {
			case domainCmdRegister:
				if cmd.unit != nil {
					units[cmd.unit.ID] = cmd.unit
				}
				domain.reply(cmd, domainReply{})
			case domainCmdRemove:
				delete(units, cmd.sid)
				domain.reply(cmd, domainReply{})
			case domainCmdUnit:
				domain.reply(cmd, domainReply{unit: units[cmd.sid]})
			case domainCmdUnitIDs:
				ids := make([]string, 0, len(units))
				for sid := range units {
					ids = append(ids, sid)
				}
				domain.reply(cmd, domainReply{ids: ids})
			case domainCmdLive:
				domain.reply(cmd, domainReply{live: len(units)})
			case domainCmdSetActive:
				activeID = cmd.sid
				domain.reply(cmd, domainReply{})
			case domainCmdActiveID:
				domain.reply(cmd, domainReply{active: activeID})
			}
		case <-domain.stopCh:
			return
		}
	}
}

func (domain *Domain) reply(cmd domainCmd, reply domainReply) {
	if cmd.reply != nil {
		cmd.reply <- reply
	}
}

func (domain *Domain) send(cmd domainCmd) {
	if domain == nil {
		return
	}
	select {
	case domain.cmds <- cmd:
	case <-domain.stopCh:
	}
}

func (domain *Domain) call(cmd domainCmd) domainReply {
	if domain == nil {
		return domainReply{}
	}
	reply := make(chan domainReply, 1)
	cmd.reply = reply
	domain.send(cmd)
	select {
	case result := <-reply:
		return result
	case <-domain.stopCh:
		return domainReply{}
	}
}

// Register 注册一个会话单元。
func (domain *Domain) Register(unit *SessionUnit) {
	domain.send(domainCmd{kind: domainCmdRegister, unit: unit, reply: make(chan domainReply, 1)})
}

// Remove 注销会话单元（unload 后调用）。
func (domain *Domain) Remove(sid string) {
	domain.send(domainCmd{kind: domainCmdRemove, sid: sid, reply: make(chan domainReply, 1)})
}

// Unit 返回指定会话单元；不存在时返回 nil。
func (domain *Domain) Unit(sid string) *SessionUnit {
	return domain.call(domainCmd{kind: domainCmdUnit, sid: sid}).unit
}

// UnitIDs 返回全部驻留会话 ID（排序由调用方负责）。
func (domain *Domain) UnitIDs() []string {
	return domain.call(domainCmd{kind: domainCmdUnitIDs}).ids
}

// Live 返回当前驻留会话数（测试与监控用）。
func (domain *Domain) Live() int {
	return domain.call(domainCmd{kind: domainCmdLive}).live
}

// SetActive 移动当前视图指针 V（hot_attach / cold_load 发布后调用）。
func (domain *Domain) SetActive(sid string) {
	domain.send(domainCmd{kind: domainCmdSetActive, sid: sid, reply: make(chan domainReply, 1)})
}

// ActiveID 返回当前视图指针指向的会话 ID。
func (domain *Domain) ActiveID() string {
	return domain.call(domainCmd{kind: domainCmdActiveID}).active
}

// Close 停止会话域 actor（幂等；Shutdown/测试 teardown 使用）。契约：
// 调用方保证无活跃操作。
func (domain *Domain) Close() {
	if domain == nil {
		return
	}
	select {
	case <-domain.stopCh:
		return
	default:
		close(domain.stopCh)
		<-domain.done
	}
}
