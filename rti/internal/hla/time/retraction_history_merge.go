package time

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// MergeRetractionHistories forms a detached exact union, preserving unique
// nonmonotonic IDs. It does not reserve IDs or change any live generation.
func MergeRetractionHistoriesOf[T core.LogicalTimeScalar](histories ...*RetractionHistoryOf[T]) (*RetractionHistoryOf[T], error) {
	producers := map[core.FederateHandle][]RetractionIDRange{}
	messages := map[core.FederateHandle][]RetractionMessageStateOf[T]{}
	for index, history := range histories {
		if err := ValidateRetractionHistoryOf[T](history); err != nil {
			return nil, err
		}
		for _, p := range history.Producers {
			producers[p.Producer] = unionRetractionRanges(producers[p.Producer], p.Accepted)
			// The first input is the execution checkpoint; later inputs carry
			// only non-reusable ID history from current consenting sessions.
			if index == 0 {
				for _, message := range p.Messages {
					messages[p.Producer] = append(messages[p.Producer], cloneRetractionMessageOf[T](message))
				}
			}
		}
	}
	out := &RetractionHistoryOf[T]{Complete: true}
	for h, ranges := range producers {
		out.Producers = append(out.Producers, RetractionProducerHistoryOf[T]{Producer: h, Accepted: ranges, Messages: messages[h]})
	}
	slices.SortFunc(out.Producers, func(a, b RetractionProducerHistoryOf[T]) int {
		if a.Producer < b.Producer {
			return -1
		}
		if a.Producer > b.Producer {
			return 1
		}
		return 0
	})
	return out, nil
}
