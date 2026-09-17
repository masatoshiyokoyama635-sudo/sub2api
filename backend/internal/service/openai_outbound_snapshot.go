package service

// snapshotOpenAIOutboundAccount freezes mutable outbound configuration for
// delayed WS dials. Scheduling state is read only; no repository account is
// changed by taking the snapshot. Outbound credential and extra fields are
// scalars, so copying their maps also isolates in-place configuration updates.
func snapshotOpenAIOutboundAccount(account *Account) *Account {
	snapshot := snapshotOAuthRefreshAccount(account)
	if snapshot == nil {
		return nil
	}
	snapshot.Extra = shallowCopyMap(account.Extra)
	if account.Proxy != nil {
		proxy := *account.Proxy
		snapshot.Proxy = &proxy
	}
	if account.ParentAccountID != nil {
		parentID := *account.ParentAccountID
		snapshot.ParentAccountID = &parentID
	}
	return snapshot
}
