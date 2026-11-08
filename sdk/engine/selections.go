package engine

// Manual choices belong to the source profile, not mihomo's process-global
// cache. Persist them across host restarts without sharing identically named
// groups between subscriptions.
func (e *Engine) selectAndSaveLocked(group, member string) error {
	previous := ""
	for _, current := range e.groupsLocked().Groups {
		if current.Name == group {
			previous = current.Now
			break
		}
	}
	if err := e.core.Select(group, member); err != nil {
		return err
	}
	profileID := e.store.ActiveID()
	selections := cloneSelections(e.manualSelections)
	if selections[profileID] == nil {
		selections[profileID] = make(map[string]string)
	}
	selections[profileID][group] = member
	if err := e.savePreferencesLocked(e.mode, selections); err != nil {
		if restoreErr := e.core.Select(group, previous); restoreErr != nil {
			return failure("preferences_failed", "无法保存节点选择且恢复失败，请断开后重新连接")
		}
		return err
	}
	e.manualSelections = selections
	return nil
}

// Call after the candidate is accepted, so a failed activation cannot prune
// choices belonging to the previous runtime. Newly removed groups or members
// retain the new configuration's default and lose their stale preference.
func (e *Engine) replaySelectionsLocked(profileID string) {
	pruned := false
	for group, member := range e.manualSelections[profileID] {
		if err := e.core.Select(group, member); err != nil {
			delete(e.manualSelections[profileID], group)
			pruned = true
		}
	}
	if len(e.manualSelections[profileID]) == 0 {
		delete(e.manualSelections, profileID)
	}
	if pruned {
		e.savePrunedPreferencesLocked()
	}
}
