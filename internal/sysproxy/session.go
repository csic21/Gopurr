package sysproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gopurr/internal/localize"
	"gopurr/internal/securefs"
)

// Session recovery is kept platform independent so crashes, takeover, foreign
// proxy changes and failed writes can be tested without touching OS settings.
type proxySessionJournal struct {
	Owner    int
	Started  uint64
	Original State
	Owned    []State
}

func (j proxySessionJournal) owns(current State) bool {
	for _, state := range j.Owned {
		if sameWindowsOptions(current, state) {
			return true
		}
	}
	return false
}

type sessionController struct {
	path    string
	owner   int
	started uint64
	current func() (State, error)
	write   func(State) error
	lock    func() (func(), error)
	alive   func(int, uint64) bool
	watch   func() error
	watched bool
}

func (c *sessionController) load() (*proxySessionJournal, error) {
	raw, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := securefs.PrivateFile(c.path); err != nil {
		return nil, err
	}
	var j proxySessionJournal
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	if j.Owner <= 1 || j.Started == 0 || len(j.Owned) == 0 || !j.Original.WindowsOptions {
		return nil, errors.New(localize.Text("系统代理恢复记录无效"))
	}
	for _, state := range j.Owned {
		if !sameWindowsOptions(state, windowsManualProxy("127.0.0.1", state.Port)) || state.Port < 1 || state.Port > 65535 {
			return nil, errors.New(localize.Text("系统代理恢复记录无效"))
		}
	}
	return &j, nil
}

func (c *sessionController) save(j *proxySessionJournal) error {
	if j == nil {
		err := os.Remove(c.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return securefs.WriteFile(c.path, raw, 0600)
}

func (c *sessionController) Enable(port int, previous *State) error {
	if port < 1 || port > 65535 {
		return errors.New(localize.Text("系统代理地址或端口无效"))
	}
	unlock, err := c.lock()
	if err != nil {
		return err
	}
	defer unlock()
	old, err := c.load()
	if err != nil {
		return err
	}
	if old != nil && (old.Owner != c.owner || old.Started != c.started) && c.alive(old.Owner, old.Started) {
		return errors.New(localize.Text("系统代理正由另一个 Gopurr 会话管理"))
	}
	current, err := c.current()
	if err != nil {
		return err
	}
	original := current
	if old != nil && old.owns(current) {
		original = old.Original
	}
	// Adopt legacy recovery records only when the OS still points at us.
	if old == nil && previous != nil && previous.WindowsOptions &&
		current.Server == "127.0.0.1" && current.Port == port && current.Enabled {
		original = *previous
	}
	desired := windowsManualProxy("127.0.0.1", port)
	j := &proxySessionJournal{Owner: c.owner, Started: c.started, Original: original, Owned: []State{desired}}
	if old != nil && old.owns(current) && !sameWindowsOptions(current, desired) {
		j.Owned = append(j.Owned, current)
	}
	if err := c.save(j); err != nil {
		return err
	}
	if !c.watched {
		if err := c.watch(); err != nil {
			return errors.Join(err, c.save(old))
		}
		c.watched = true
	}
	if err := c.write(desired); err != nil {
		// Keep the recovery journal if rollback fails: the watcher/next launch
		// must still be able to release settings changed by a partial write.
		if restoreErr := c.write(current); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return errors.Join(err, c.save(old))
	}
	observed, err := c.current()
	if err != nil || !sameWindowsOptions(observed, desired) {
		return errors.Join(errors.New(localize.Text("系统代理设置未生效，请检查系统策略或其他代理软件")), err)
	}
	return nil
}

func (c *sessionController) restoreLocked(j *proxySessionJournal) error {
	current, err := c.current()
	if err != nil {
		return err
	}
	// Never overwrite a subsequent change made by the user, a VPN or policy.
	if j.owns(current) {
		if err := c.write(j.Original); err != nil {
			return err
		}
		observed, err := c.current()
		if err != nil {
			return err
		}
		if !sameWindowsOptions(observed, j.Original) {
			return errors.New(localize.Text("系统代理恢复未生效，请检查系统策略"))
		}
	}
	return c.save(nil)
}

func (c *sessionController) Restore(port int, previous *State) error {
	unlock, err := c.lock()
	if err != nil {
		return err
	}
	defer unlock()
	j, err := c.load()
	if err != nil {
		return err
	}
	if j != nil {
		if (j.Owner != c.owner || j.Started != c.started) && c.alive(j.Owner, j.Started) {
			return nil
		}
		return c.restoreLocked(j)
	}
	current, err := c.current()
	if err != nil {
		return err
	}
	if port <= 0 || current.Server != "127.0.0.1" || current.Port != port || !current.Enabled {
		return nil
	}
	if previous != nil {
		return c.write(*previous)
	}
	current.Flags = (current.Flags &^ proxyTypeProxy) | proxyTypeDirect
	return c.write(current)
}

func (c *sessionController) Close() error { return c.Restore(0, nil) }

func (c *sessionController) Recover() error {
	unlock, err := c.lock()
	if err != nil {
		return err
	}
	defer unlock()
	j, err := c.load()
	if err != nil {
		return fmt.Errorf(localize.Text("读取系统代理恢复记录失败: %w"), err)
	}
	if j == nil || c.alive(j.Owner, j.Started) {
		return nil
	}
	return c.restoreLocked(j)
}
