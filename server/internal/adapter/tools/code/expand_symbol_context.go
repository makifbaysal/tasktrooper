package code

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const expandSymbolContextToolName = "expand_symbol_context"

type expandSymbolContextArgs struct {
	SymbolName string `json:"symbol_name"`
	FilePath   string `json:"file_path"`
	Depth      int    `json:"depth"`
	MaxChunks  int    `json:"max_chunks"`
}

type expandedChunk struct {
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Signature  string `json:"signature"`
	Snippet    string `json:"snippet"`
}

type expandSymbolContextResponse struct {
	Symbols []string        `json:"symbols"`
	Chunks  []expandedChunk `json:"chunks"`
}

type expandSymbolContextTool struct {
	kit *ToolKit
}

func newExpandSymbolContextTool(kit *ToolKit) port.ToolExecutor {
	return &expandSymbolContextTool{kit: kit}
}

func (t *expandSymbolContextTool) Name() string {
	return expandSymbolContextToolName
}

func (t *expandSymbolContextTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: expandSymbolContextToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"symbol_name": map[string]interface{}{
						"type": "string",
					},
					"file_path": map[string]interface{}{
						"type": "string",
					},
					"depth": map[string]interface{}{
						"type": "integer",
					},
					"max_chunks": map[string]interface{}{
						"type": "integer",
					},
				},
				"required": []string{"symbol_name"},
			},
		},
	}
}

func (t *expandSymbolContextTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args expandSymbolContextArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(expandSymbolContextToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.SymbolName == "" {
		return toolError(expandSymbolContextToolName, "symbol_name is required")
	}
	args.FilePath = workspace.IndexPath(registry.EffectiveWorkspaceDir(ctx), args.FilePath)
	if t.kit.IndexStore == nil {
		return toolError(expandSymbolContextToolName, "index store not configured")
	}

	idx, err := resolveIndex(ctx, t.kit.IndexStore)
	if err != nil {
		return indexUnavailableError(expandSymbolContextToolName, err)
	}

	symbols, err := t.kit.IndexStore.SearchSymbols(ctx, args.SymbolName, idx.ID)
	if err != nil {
		return toolError(expandSymbolContextToolName, fmt.Sprintf("search symbols: %v", err))
	}

	edges, err := t.kit.IndexStore.ListEdges(ctx, idx.ID)
	if err != nil {
		return toolError(expandSymbolContextToolName, fmt.Sprintf("list edges: %v", err))
	}

	// Unindexed edits: stale symbols/edges of touched files are replaced by a
	// live parse, so the graph walks the tree as the agent just left it.
	overlay := buildOverlay(ctx, t.kit, idx)
	if overlay != nil {
		keptSyms := symbols[:0]
		for _, sym := range symbols {
			if !overlay.isStale(sym.FilePath) {
				keptSyms = append(keptSyms, sym)
			}
		}
		symbols = append(keptSyms, overlay.symbolsNamed(args.SymbolName)...)

		keptEdges := edges[:0]
		for _, e := range edges {
			if !overlay.isStale(e.FromFile) {
				keptEdges = append(keptEdges, e)
			}
		}
		edges = append(keptEdges, overlay.liveEdges()...)
	}
	if len(symbols) == 0 {
		return toolError(expandSymbolContextToolName, "symbol not found in index")
	}

	target := symbols[0]
	if args.FilePath != "" {
		for _, sym := range symbols {
			if sym.FilePath == args.FilePath {
				target = sym
				break
			}
		}
	}

	g := buildGraphFromEdges(edges)
	lookup := indexSymbolLookup{symbols: symbols}

	depth := args.Depth
	if depth <= 0 {
		depth = t.kit.GraphCfg.MaxExpansionDepth
		if depth <= 0 {
			depth = 2
		}
	}
	maxChunks := args.MaxChunks
	if maxChunks <= 0 {
		maxChunks = t.kit.GraphCfg.MaxExpandedChunks
		if maxChunks <= 0 {
			maxChunks = 8
		}
	}

	targetRef := graph.SymbolRef{
		FilePath:   target.FilePath,
		SymbolName: target.Name,
		Kind:       target.Kind,
	}

	refs, keys := graph.ExpandContext(g, lookup, targetRef, target.FilePath, depth, maxChunks)

	symbolNames := make([]string, 0, len(keys))
	for _, ref := range refs {
		symbolNames = append(symbolNames, ref.FilePath+":"+ref.SymbolName)
	}

	chunks := make([]expandedChunk, 0, len(refs))
	for _, ref := range refs {
		if overlay.isStale(ref.FilePath) {
			if live, ok := overlay.chunkFor(ref.FilePath, ref.SymbolName); ok {
				chunks = append(chunks, expandedChunk{
					FilePath:   live.FilePath,
					SymbolName: live.SymbolName,
					StartLine:  live.StartLine,
					EndLine:    live.EndLine,
					Signature:  live.Signature,
					Snippet:    live.Content,
				})
			}
			continue
		}
		ch, err := t.kit.IndexStore.GetChunkBySymbol(ctx, idx.ID, ref.FilePath, ref.SymbolName)
		if err != nil {
			continue
		}
		snippet := ch.Content
		if snippet == "" {
			snippet = ch.Signature
		}
		chunks = append(chunks, expandedChunk{
			FilePath:   ch.FilePath,
			SymbolName: ch.SymbolName,
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			Signature:  ch.Signature,
			Snippet:    snippet,
		})
	}

	return toolJSON(expandSymbolContextToolName, expandSymbolContextResponse{
		Symbols: symbolNames,
		Chunks:  chunks,
	})
}
