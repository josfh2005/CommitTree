package merge

import "context"

// Fingerprint identifies the operation in progress as "<kind>:<commit>", so
// something started for one (an AI resolver run) can tell when it is gone.
// A rebase is keyed by the commit being replayed, so each stopped step is a
// different operation. It checks the markers in Status's order and returns
// "" when nothing with a marker is in progress (a stash conflict has none).
func Fingerprint(ctx context.Context, dir string) string {
	for _, k := range []struct {
		kind Kind
		ref  string
	}{{KindCherryPick, "CHERRY_PICK_HEAD"}, {KindRevert, "REVERT_HEAD"}, {KindMerge, "MERGE_HEAD"}} {
		if h := pickedCommit(ctx, dir, k.ref); h != "" {
			return string(k.kind) + ":" + h
		}
	}
	rebaseDir, ok := inRebase(ctx, dir)
	if !ok {
		return ""
	}
	h, _ := head(ctx, dir)
	if isApplyingMailbox(rebaseDir) {
		return string(KindAM) + ":" + h
	}
	if replayed := pickedCommit(ctx, dir, "REBASE_HEAD"); replayed != "" {
		return string(KindRebase) + ":" + replayed
	}
	return string(KindRebase) + ":" + h // older backend: HEAD moves with every step
}
