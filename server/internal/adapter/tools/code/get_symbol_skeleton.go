package code

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const getSymbolSkeletonToolName = "get_symbol_skeleton"

type getSymbolSkeletonArgs struct {
	FilePath   string `json:"file_path"`
	SymbolName string `json:"symbol_name"`
}

type symbolSkeletonEntry struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type getSymbolSkeletonResponse struct {
	FilePath string                `json:"file_path"`
	Package  string                `json:"package"`
	Symbols  []symbolSkeletonEntry `json:"symbols"`
}

type getSymbolSkeletonTool struct {
	kit *ToolKit
}

func newGetSymbolSkeletonTool(kit *ToolKit) port.ToolExecutor {
	return &getSymbolSkeletonTool{kit: kit}
}

func (t *getSymbolSkeletonTool) Name() string {
	return getSymbolSkeletonToolName
}

func (t *getSymbolSkeletonTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getSymbolSkeletonToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type": "string",
					},
					"symbol_name": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *getSymbolSkeletonTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args getSymbolSkeletonArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getSymbolSkeletonToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.FilePath == "" && args.SymbolName == "" {
		return toolError(getSymbolSkeletonToolName, "file_path or symbol_name is required")
	}
	args.FilePath = workspace.IndexPath(registry.EffectiveWorkspaceDir(ctx), args.FilePath)

	if args.SymbolName != "" && t.kit.IndexStore != nil {
		resp, err := t.skeletonFromIndex(ctx, args)
		if err == nil {
			return toolJSON(getSymbolSkeletonToolName, resp)
		}
	}

	if args.FilePath == "" {
		return toolError(getSymbolSkeletonToolName, "file_path is required when index lookup is unavailable")
	}
	if t.kit.Mapper == nil {
		return toolError(getSymbolSkeletonToolName, "mapper not configured")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(getSymbolSkeletonToolName, err.Error())
	}

	// Same containment rule as grep_code, and for the same reason: FileSkeleton
	// joins this path onto the root and reads the file, so "../../../../etc/
	// passwd" turned a read-only code tool into an arbitrary file reader for
	// the very API key whose policy withholds run_terminal. The mapper enforces
	// this too; rejecting here is what produces an error the agent can act on
	// instead of a "file skeleton: ..." wrapper.
	if _, err := workspace.ResolveWithinRoot(root, args.FilePath); err != nil {
		return toolError(getSymbolSkeletonToolName, err.Error())
	}

	sk, err := t.kit.Mapper.FileSkeleton(root, args.FilePath)
	if err != nil {
		return toolError(getSymbolSkeletonToolName, fmt.Sprintf("file skeleton: %v", err))
	}

	resp := fileSkeletonToResponse(sk, args.SymbolName)
	return toolJSON(getSymbolSkeletonToolName, resp)
}

func (t *getSymbolSkeletonTool) skeletonFromIndex(ctx context.Context, args getSymbolSkeletonArgs) (getSymbolSkeletonResponse, error) {
	idx, err := resolveIndex(ctx, t.kit.IndexStore)
	if err != nil {
		return getSymbolSkeletonResponse{}, err
	}

	symbols, err := t.kit.IndexStore.SearchSymbols(ctx, args.SymbolName, idx.ID)
	if err != nil {
		return getSymbolSkeletonResponse{}, err
	}
	// Unindexed edits win over stale index rows for the files they touched.
	if overlay := buildOverlay(ctx, t.kit, idx); overlay != nil {
		kept := symbols[:0]
		for _, sym := range symbols {
			if !overlay.isStale(sym.FilePath) {
				kept = append(kept, sym)
			}
		}
		symbols = append(kept, overlay.symbolsNamed(args.SymbolName)...)
	}
	if len(symbols) == 0 {
		return getSymbolSkeletonResponse{}, fmt.Errorf("symbol not found")
	}

	filePath := args.FilePath
	if filePath == "" {
		filePath = symbols[0].FilePath
	}

	var entries []symbolSkeletonEntry
	for _, sym := range symbols {
		if sym.FilePath != filePath {
			continue
		}
		if args.SymbolName != "" && sym.Name != args.SymbolName {
			continue
		}
		entries = append(entries, symbolSkeletonEntry{
			Kind:      sym.Kind,
			Name:      sym.Name,
			Signature: sym.Signature,
			Doc:       sym.Doc,
			StartLine: sym.StartLine,
			EndLine:   sym.EndLine,
		})
	}
	if len(entries) == 0 {
		return getSymbolSkeletonResponse{}, fmt.Errorf("symbol not found in file")
	}

	return getSymbolSkeletonResponse{
		FilePath: filePath,
		Symbols:  entries,
	}, nil
}

func fileSkeletonToResponse(sk mapper.FileSkeleton, symbolName string) getSymbolSkeletonResponse {
	entries := make([]symbolSkeletonEntry, 0, len(sk.Symbols))
	for _, sym := range sk.Symbols {
		if symbolName != "" && sym.Name != symbolName {
			continue
		}
		sig := sym.Name
		if sym.Receiver != "" {
			sig = fmt.Sprintf("(%s) %s", sym.Receiver, sym.Name)
		}
		entries = append(entries, symbolSkeletonEntry{
			Kind:      sym.Kind,
			Name:      sym.Name,
			Signature: sig,
			Doc:       strings.TrimSpace(sym.Doc),
		})
	}
	return getSymbolSkeletonResponse{
		FilePath: sk.Path,
		Package:  sk.Package,
		Symbols:  entries,
	}
}
