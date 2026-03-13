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
	StatusCancelled
	StatusPaused
)

type NodeOp = int

const (
	OpNone NodeOp = iota
	OpFreeze
	OpMelt
	OpRun
	OpFail
	OpSignal
	OpCancel
	OpSuccess
)

var AllOps = []NodeOp{OpFreeze, OpMelt, OpRun, OpFail, OpSignal, OpCancel, OpSuccess}

func StatusActive(st Status) bool {
	return st == StatusRunning
}

func StatusHoldsLocks(st Status) bool {
	return st == StatusRunning || st == StatusPaused
}

func StatusFinished(st Status) bool {
	return st == StatusSuccess || st == StatusCancelled || st == StatusFailed
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
//	case StatusCancelled:
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
	case StatusCancelled:
		return "Cancelled"
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
		return "Frozen, the scheduler will not consider starting the node until it is melted manually."
	case StatusRunning:
		return "Running, it has at least one process running."
	case StatusSuccess:
		return "Success, the node was completed successfully."
	case StatusFailed:
		return "Failed, the node was not completed successfully."
	case StatusCancelled:
		return "Cancelled, the node will never be stated by the scheduler."
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
	case OpMelt:
		return "Melt"
	case OpRun:
		return "Run"
	case OpFail:
		return "Fail"
	case OpSignal:
		return "Signal"
	case OpCancel:
		return "Cancel"
	case OpSuccess:
		return "Success"
	default:
		return "?"
	}
}

const TermEmoji = "🖵"
const LockEmoji = "🔒"
const UnlockEmoji = "🔓"
