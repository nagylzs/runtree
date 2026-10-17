package rt

import (
	"slices"
	"strings"
)

// ProvidedResource is a provided resource that would be unprovided by a reset to waiting operation
type ProvidedResource struct {
	Name       string
	ProvidedBy []*Node // the nodes that provide it now, they will not provide it after the reset
	RequiredBy []*Node // the nodes that require it
}

// ResetPlan tells what a reset to waiting operation would do, see Node.PlanResetToWaiting
type ResetPlan struct {
	Unprovide []ProvidedResource // resources that would not be provided anymore after the reset, sorted by name
	Running   []*Node            // running nodes in the subtree, they would not be reset
}

// providerStatus tells if a node in this status has provided its resources (see Node.updateStatus)
func providerStatus(st Status) bool {
	return StatusFinished(st) && !StatusHasError(st)
}

// PlanResetToWaiting tells what a reset to waiting operation would do on the node, before it is performed:
// which provided resources would be unprovided, and which running nodes would not be reset.
//
// A provided resource is unprovided when all the nodes that provide it are reset, or become unfinished because
// some of their sub-nodes are reset. The plan is only used for displaying: PerformOp decides what to unprovide by
// comparing the statuses before and after the reset, so the result may differ in rare cases (e.g. when a skipped
// seq/par node is reset, and it becomes successful again right away).
func (n *Node) PlanResetToWaiting(locked bool, recursive bool) ResetPlan {
	if !locked {
		panic("must be called in locked state")
	}
	var plan ResetPlan
	lost := make(map[*Node]bool) // provider nodes that will not be providers after the reset

	// walk visits the nodes the same way as performOp does, and returns true if something is reset in the subtree
	var walk func(node *Node, recursive bool) bool
	walk = func(node *Node, recursive bool) bool {
		_, allowed := node.AllowedManualOps(true, recursive)[OpResetToWaiting]
		if !allowed {
			if node.Type == TypeRun && StatusActive(node.Status) {
				plan.Running = append(plan.Running, node)
			}
			return false
		}
		// run nodes are always reset when allowed, seq/par nodes only change by themselves in these statuses
		ownReset := node.Type == TypeRun ||
			node.Status == StatusFrozen || node.Status == StatusPaused || node.Status == StatusSkipped
		subReset := false
		if recursive {
			for _, sub := range node.Nodes {
				if walk(sub, sub.HasChild()) {
					subReset = true
				}
			}
		}
		if providerStatus(node.Status) && (node.Type == TypeRun || node.Status == StatusSkipped || subReset) {
			lost[node] = true
		}
		return ownReset || subReset
	}
	if !walk(n, recursive) {
		return plan
	}
	// successful parents become unfinished too (skipped ones stay skipped)
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Status == StatusSuccess {
			lost[p] = true
		}
	}

	for _, name := range n.Tree.provided.List() {
		var providedBy, requiredBy []*Node
		stays := false
		for _, node := range n.Tree.Nodes {
			if node.Calculated.Requires.Contains(name) {
				requiredBy = append(requiredBy, node)
			}
			if !providerStatus(node.Status) || !node.Calculated.Provides.Contains(name) {
				continue
			}
			if lost[node] {
				providedBy = append(providedBy, node)
			} else {
				stays = true
			}
		}
		if len(providedBy) > 0 && !stays {
			plan.Unprovide = append(plan.Unprovide, ProvidedResource{
				Name:       name,
				ProvidedBy: providedBy,
				RequiredBy: requiredBy,
			})
		}
	}
	slices.SortFunc(plan.Unprovide, func(a, b ProvidedResource) int {
		return strings.Compare(a.Name, b.Name)
	})
	return plan
}

// providerNodes returns the nodes that have provided their resources
func (t *Tree) providerNodes() map[*Node]bool {
	result := make(map[*Node]bool)
	for _, node := range t.Nodes {
		if providerStatus(node.Status) {
			result[node] = true
		}
	}
	return result
}

// unprovideLost removes the provided resources that were provided by the "before" provider nodes, but are not
// provided by any provider node anymore. When names is not nil, then only the listed resources can be removed.
// It returns the removed resources.
func (t *Tree) unprovideLost(before map[*Node]bool, names []string) []string {
	after := t.providerNodes()
	var removed []string
	for _, name := range t.provided.List() {
		if names != nil && !slices.Contains(names, name) {
			continue
		}
		var lostProvider bool
		var provider *Node
		for node := range before {
			if !after[node] && node.Calculated.Provides.Contains(name) {
				lostProvider = true
			}
		}
		for node := range after {
			if node.Calculated.Provides.Contains(name) {
				provider = node
			}
		}
		if !lostProvider {
			continue
		}
		if provider != nil {
			// still provided by another node
			if !after[t.providedBy[name]] {
				t.providedBy[name] = provider
			}
			continue
		}
		t.provided.Remove(name)
		delete(t.providedBy, name)
		removed = append(removed, name)
	}
	if len(removed) > 0 {
		t.Root.updateBlockedAll(true)
		t.Awake()
	}
	slices.Sort(removed)
	return removed
}
