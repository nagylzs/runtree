package rt

import "testing"

// conditionTree is the "reboot if needed" pattern: the first step is a condition for the next ones
const conditionTree = `
tree:
  id: root
  seq:
    - id: reboot
      seq:
        - {id: needed, on_error: [success, skip], args: [test, -f, /var/run/reboot-required]}
        - {id: do_reboot, args: [reboot]}
        - {id: wait, args: ["true"]}
    - {id: after, args: ["true"]}
`

// on_error: [success, skip]: when the condition fails, the rest of its seq is skipped, and the tree goes on
func TestOnErrorConditionFalse(t *testing.T) {
	s := newSchedTree(t, conditionTree)
	s.expect("needed")
	s.run("needed")
	s.finish(StatusFailed, "needed")
	s.expectStatuses("Success Skipped Skipped Success", "needed", "do_reboot", "wait", "reboot")
	s.expect("after")
}

// on_error: [success, skip]: when the condition is true, the next steps run
func TestOnErrorConditionTrue(t *testing.T) {
	s := newSchedTree(t, conditionTree)
	s.run("needed")
	s.finish(StatusSuccess, "needed")
	s.expectStatuses("Waiting Waiting Running", "do_reboot", "wait", "reboot")
	s.expect("do_reboot")
}

// on_error: [failed, skip] in a par node: the siblings that are not started are skipped, the running ones finish, then
// the par node fails
func TestOnErrorSkipSiblings(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  max_proc: 2
  par:
    - {id: a, on_error: [failed, skip], args: ["false"]}
    - {id: b, args: ["true"]}
    - {id: c, args: ["true"]}
    - {id: d, args: ["true"]}
`)
	s.expect("a")
	s.run("a")
	s.expect("b")
	s.run("b")
	s.expect("") // max_proc
	s.finish(StatusFailed, "a")
	s.expectStatuses("Failed Running Skipped Skipped Running", "a", "b", "c", "d", "root")
	s.expect("")
	s.finish(StatusSuccess, "b")
	s.expectStatuses("Failed", "root")
}

// on_error: [paused, freeze]: the siblings stay frozen when the paused node is set to success, until they are thawed
func TestOnErrorPausedFreeze(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  max_proc: 1
  par:
    - {id: s1, on_error: [paused, freeze], args: ["false"]}
    - {id: s2, args: ["true"]}
    - {id: s3, args: ["true"]}
`)
	s.run("s1")
	s.finish(StatusFailed, "s1")
	s.expectStatuses("Paused Frozen Frozen Running", "s1", "s2", "s3", "root")
	op(s, "s1", OpSuccess, false)
	s.expectStatuses("Success Frozen Frozen Running", "s1", "s2", "s3", "root")
	s.expect("")
	op(s, "root", OpThaw, true)
	s.expectStatuses("Waiting Waiting", "s2", "s3")
	s.expect("s2")
}

// on_error: [success, freeze]: the node counts as successful, but the next steps wait for an approval (Thaw)
func TestOnErrorSuccessFreeze(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  seq:
    - {id: check, on_error: [success, freeze], args: ["false"]}
    - {id: deploy, args: ["true"]}
    - {id: verify, args: ["true"]}
`)
	s.run("check")
	s.finish(StatusFailed, "check")
	s.expectStatuses("Success Frozen Frozen Running", "check", "deploy", "verify", "root")
	s.expect("")
	op(s, "deploy", OpThaw, false)
	s.expect("deploy")
	s.run("deploy")
	s.finish(StatusSuccess, "deploy")
	s.expect("") // verify is still frozen
	op(s, "verify", OpSkip, false)
	s.expectStatuses("Success", "root")
}

// The sibling operation of a seq/par node: when a host group fails, the host groups that are not started are frozen
// (and they are Frozen themselves), the tree waits for a decision
func TestOnErrorFreezeGroups(t *testing.T) {
	s := newSchedTree(t, `
tree:
  id: root
  max_proc: 1
  children_on_error: [failed, freeze]
  par:
    - id: h1
      children_on_error: failed
      seq: [{id: h1a, args: ["true"]}, {id: h1b, args: ["true"]}]
    - id: h2
      children_on_error: failed
      seq: [{id: h2a, args: ["true"]}, {id: h2b, args: ["true"]}]
    - id: h3
      children_on_error: failed
      seq: [{id: h3a, args: ["true"]}, {id: h3b, args: ["true"]}]
`)
	s.run("h1a")
	s.finish(StatusSuccess, "h1a")
	s.run("h1b")
	s.finish(StatusFailed, "h1b")
	s.expectStatuses("Failed Frozen Frozen Frozen Frozen Frozen Frozen", "h1", "h2", "h2a", "h2b", "h3", "h3a",
		"h3b")
	s.expectStatuses("Running", "root") // until the frozen hosts are thawed or skipped
	s.expect("")
	op(s, "h2", OpThaw, true)
	s.expect("h2a")
	op(s, "h3", OpSkip, true)
	s.run("h2a")
	s.finish(StatusSuccess, "h2a")
	s.run("h2b")
	s.finish(StatusSuccess, "h2b")
	s.expectStatuses("Success Skipped Failed", "h2", "h3", "root")
}
