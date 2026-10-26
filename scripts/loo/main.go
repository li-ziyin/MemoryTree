// Command loo measures leave-one-out mount accuracy for a MemoryTree data
// repository: every entry is offered to the mount scorer as if it were new
// (itself excluded from scoring), and the scorer's top candidate is compared
// with the entry's actual node.
//
// Usage: go run ./scripts/loo <data-dir>
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/li-ziyin/memorytree/internal/mount"
	"github.com/li-ziyin/memorytree/internal/validate"
)

func main() {
	root := os.Args[1]
	res, snap, err := validate.ValidateWithSnapshot(root)
	if err != nil {
		panic(err)
	}
	if !res.OK() {
		panic("validation errors")
	}

	total, top1, top3 := 0, 0, 0
	rankSum := 0
	byType := map[string][2]int{} // type -> {total, top1}
	for _, e := range snap.Entries {
		view := mount.FromSnapshot(snap, e.ID)
		sugg := mount.Suggest(view, mount.Query{
			Type:    e.Type,
			Title:   e.Title,
			Summary: e.Summary,
			Tags:    lower(e.Tags),
		}, 5)
		total++
		rank := 0
		for i, s := range sugg {
			if s.Path == e.NodePath {
				rank = i + 1
				break
			}
		}
		correct := rank == 1
		if correct {
			top1++
		}
		if rank > 0 && rank <= 3 {
			top3++
		}
		if rank > 0 {
			rankSum += rank
		} else {
			rankSum += len(sugg) + 1
		}
		pair := byType[e.Type]
		pair[0]++
		if correct {
			pair[1]++
		}
		byType[e.Type] = pair
		status := "MISS"
		if rank > 0 {
			status = fmt.Sprintf("rank %d", rank)
		}
		fmt.Printf("%-9s %-9s top=%-18s want=%-22s %s\n", status, e.Type, firstPath(sugg), e.NodePath, e.Title)
	}
	fmt.Printf("\ntotal %d, top1 %d (%.1f%%), top3 %d (%.1f%%), mean rank %.2f\n",
		total, top1, pct(top1, total), top3, pct(top3, total), float64(rankSum)/float64(total))
	for _, t := range []string{"user", "feedback", "project", "reference"} {
		if pair, ok := byType[t]; ok {
			fmt.Printf("  %-9s %d/%d\n", t, pair[1], pair[0])
		}
	}
}

func lower(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, strings.ToLower(t))
	}
	return out
}

func firstPath(sugg []mount.Suggestion) string {
	if len(sugg) == 0 {
		return "(none)"
	}
	return sugg[0].Path
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}
