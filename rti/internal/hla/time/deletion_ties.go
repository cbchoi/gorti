package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type objectReflectionEvent interface {
	ReflectionObject() core.ObjectHandle
}

type objectDeletionTieOf[T core.LogicalTimeScalar] struct {
	slots       []int
	reflections []bufferedTSOEventOf[T]
	deletions   []bufferedTSOEventOf[T]
}

// Equal timestamps need no universal kind ordering. Only partition the slots
// belonging to one object's reflections/removal, leaving unrelated slots fixed.
// The input is already timestamp-sorted; RO queues do not use this adjustment.
func orderReflectionDeletionTies[T core.LogicalTimeScalar](release []bufferedTSOEventOf[T]) {
	for start := 0; start < len(release); {
		end := start + 1
		for end < len(release) && release[end].timestamp == release[start].timestamp {
			end++
		}
		orderObjectDeletionTies(release[start:end])
		start = end
	}
}

func orderObjectDeletionTies[T core.LogicalTimeScalar](release []bufferedTSOEventOf[T]) {
	objects := collectDeletionTies(release)
	if len(objects) != 0 {
		for i := 0; i < len(release); i++ {
			item := release[i]
			var obj core.ObjectHandle
			if reflection, ok := item.event.(objectReflectionEvent); ok {
				obj = reflection.ReflectionObject()
			}
			if group := objects[obj]; obj != 0 && group != nil {
				group.slots = append(group.slots, i)
				group.reflections = append(group.reflections, item)
				continue
			}
			if deletion, ok := item.event.(core.ObjectDeletionEvent); ok {
				if group := objects[deletion.DeletionObject()]; group != nil {
					group.slots = append(group.slots, i)
					group.deletions = append(group.deletions, item)
				}
			}
		}
		for _, group := range objects {
			if len(group.reflections) == 0 {
				continue
			}
			for i, item := range group.reflections {
				release[group.slots[i]] = item
			}
			for i, item := range group.deletions {
				release[group.slots[len(group.reflections)+i]] = item
			}
		}
	}
}

func collectDeletionTies[T core.LogicalTimeScalar](release []bufferedTSOEventOf[T]) map[core.ObjectHandle]*objectDeletionTieOf[T] {
	var objects map[core.ObjectHandle]*objectDeletionTieOf[T]
	for _, item := range release {
		if deletion, ok := item.event.(core.ObjectDeletionEvent); ok {
			if obj := deletion.DeletionObject(); obj != 0 {
				if objects == nil {
					objects = make(map[core.ObjectHandle]*objectDeletionTieOf[T])
				}
				if objects[obj] == nil {
					objects[obj] = &objectDeletionTieOf[T]{}
				}
			}
		}
	}
	return objects
}
