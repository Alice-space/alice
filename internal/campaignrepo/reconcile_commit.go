package campaignrepo

type ReconcileCommitResult struct {
	RepoCommit          string
	RepoCommitted       bool
	LiveReportPath      string
	LiveReportCommit    string
	LiveReportCommitted bool
}

// CommitReconcileSnapshot first commits task/campaign state changes produced by
// reconcile, then refreshes and separately commits the global live report. This
// keeps task-touching commits isolated from campaign-wide report refreshes so
// reviewer write-scope checks can inspect a task-local commit boundary.
func CommitReconcileSnapshot(root string, summary *Summary) (ReconcileCommitResult, error) {
	var result ReconcileCommitResult

	commit, committed, err := CommitRepoChanges(root, "chore(campaign): reconcile repo state")
	if err != nil {
		return result, err
	}
	result.RepoCommit = commit
	result.RepoCommitted = committed

	if summary == nil {
		return result, nil
	}

	path, err := WriteLiveReport(root, *summary)
	if err != nil {
		return result, err
	}
	result.LiveReportPath = path

	commit, committed, err = CommitRepoChanges(root, "chore(campaign): refresh live report")
	if err != nil {
		return result, err
	}
	result.LiveReportCommit = commit
	result.LiveReportCommitted = committed
	return result, nil
}
