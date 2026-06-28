package rt

type Type = int

const (
	TypeRun Type = iota
	TypeSeq
	TypePar
)

type Status = int

func TypeName(s Status) string {
	switch s {
	case TypeRun:
		return "Run"
	case TypeSeq:
		return "Seq"
	case TypePar:
		return "Par"
	default:
		return "Unknown"
	}
}

const (
	StatusWaiting Status = iota
	StatusFrozen
	StatusRunning
	StatusSuccess
	StatusFailed
	StatusSkipped
	StatusPaused
)

type NodeOp = int

const (
	OpNone NodeOp = iota
	OpFreeze
	OpThaw
	OpRun
	OpFail
	OpSignal
	OpSkip
	OpSuccess
	OpResetToWaiting
)

var AllOps = []NodeOp{OpFreeze, OpThaw, OpRun, OpFail, OpSignal, OpSkip, OpSuccess, OpResetToWaiting}

func StatusActive(st Status) bool {
	return st == StatusRunning
}

func StatusHoldsLocks(st Status) bool {
	return st == StatusRunning || st == StatusPaused
}

func StatusFinished(st Status) bool {
	return st == StatusSuccess || st == StatusSkipped || st == StatusFailed
}

func StatusHasError(st Status) bool {
	return st == StatusFailed
}

//func StatusEmoji(st Status, blocked bool) string {
//	// https://www.compart.com/en/unicode/search?q=check#characters
//	// https://www.iemoji.com/view/emoji/2260/symbols/stop-sign
//	switch st {
//	case StatusWaiting:
//
//		if blocked {
//			return LockEmoji
//		} else {
//			return " "
//		}
//	case StatusFrozen:
//		return "❄"
//	case StatusRunning:
//		return "🏃"
//	case StatusSuccess:
//		return "✓"
//	case StatusFailed:
//		return "⚠"
//	case StatusSkipped:
//		return "✘"
//	case StatusPaused:
//		return "⏸"
//	default:
//		return "?"
//	}
//}

func StatusName(st Status) string {
	switch st {
	case StatusWaiting:
		return "Waiting"
	case StatusFrozen:
		return "Frozen"
	case StatusRunning:
		return "Running"
	case StatusSuccess:
		return "Success"
	case StatusFailed:
		return "Failed"
	case StatusSkipped:
		return "Skipped"
	case StatusPaused:
		return "Paused"
	default:
		return "?"
	}
}

func StatusDescription(st Status) string {
	switch st {
	case StatusWaiting:
		return "Waiting for the scheduler to start the node"
	case StatusFrozen:
		return "Frozen, the scheduler will not consider starting the node until it is thawed manually."
	case StatusRunning:
		return "Running, it has at least one process running."
	case StatusSuccess:
		return "Success, the node was completed successfully."
	case StatusFailed:
		return "Failed, the node was not completed successfully."
	case StatusSkipped:
		return "Skipped, the node will never be stated by the scheduler."
	case StatusPaused:
		return "Paused, the process(es) finished, but the next status must be chosen manually."
	default:
		return "?"
	}
}

func OpName(st NodeOp) string {
	switch st {
	case OpNone:
		return "None"
	case OpFreeze:
		return "Freeze"
	case OpThaw:
		return "Thaw"
	case OpRun:
		return "Run"
	case OpFail:
		return "Fail"
	case OpSignal:
		return "Signal"
	case OpSkip:
		return "Skip"
	case OpSuccess:
		return "Success"
	case OpResetToWaiting:
		return "ResetToWaiting"
	default:
		return "?"
	}
}

const TermEmoji = "🖵"
const LockEmoji = "🔒"
const UnlockEmoji = "🔓"
