package graph

func ExpandContext(g *DependencyGraph, lookup SymbolLookup, target SymbolRef, filePath string, depth int, maxChunks int) ([]SymbolRef, []string) {
	if g == nil || maxChunks <= 0 {
		return nil, nil
	}

	type queueItem struct {
		ref   SymbolRef
		depth int
	}

	visited := make(map[string]struct{})
	keys := make(map[string]struct{})
	var refs []SymbolRef
	var dedupeKeys []string

	start := target
	if start.FilePath == "" {
		start.FilePath = filePath
	}

	queue := []queueItem{{ref: start, depth: 0}}
	visited[start.Key()] = struct{}{}
	refs = append(refs, start)
	keys[start.Key()] = struct{}{}
	dedupeKeys = append(dedupeKeys, start.Key())

	for len(queue) > 0 && len(refs) < maxChunks {
		item := queue[0]
		queue = queue[1:]

		if item.depth >= depth {
			continue
		}

		calls := g.OutgoingCalls(item.ref)
		for _, callee := range calls {
			resolved := callee
			if lookup != nil {
				if found, ok := lookup.Lookup(item.ref.FilePath, callee.SymbolName); ok {
					resolved = found
				} else if found, ok := lookup.Lookup(filePath, callee.SymbolName); ok {
					resolved = found
				}
			}
			if resolved.FilePath == "" {
				resolved.FilePath = item.ref.FilePath
			}

			key := resolved.Key()
			if _, seen := visited[key]; seen {
				continue
			}
			if len(refs) >= maxChunks {
				break
			}

			visited[key] = struct{}{}
			refs = append(refs, resolved)
			if _, seenKey := keys[key]; !seenKey {
				keys[key] = struct{}{}
				dedupeKeys = append(dedupeKeys, key)
			}
			queue = append(queue, queueItem{ref: resolved, depth: item.depth + 1})
		}
	}

	return refs, dedupeKeys
}
