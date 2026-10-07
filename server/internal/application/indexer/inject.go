package indexer

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var queryRewriteSystemKey = prompt.Define[struct{}]("indexer.query_rewrite_system", struct{}{})

type Injector struct {
	store          port.IndexStore
	llm            port.LLMClient
	mapper         *mapper.Service
	embeddingModel string
	graphCfg       domain.GraphConfig
	rewriteEnabled bool
	skeletons      *skeletonCache
}

func (i *Injector) SetQueryRewrite(enabled bool) {
	i.rewriteEnabled = enabled
}

func NewInjector(
	store port.IndexStore,
	llm port.LLMClient,
	mapperSvc *mapper.Service,
	embeddingModel string,
	graphCfg domain.GraphConfig,
) *Injector {
	return &Injector{
		store:          store,
		llm:            llm,
		mapper:         mapperSvc,
		embeddingModel: embeddingModel,
		graphCfg:       graphCfg,
		skeletons:      newSkeletonCache(skeletonCacheEntries),
	}
}

func (i *Injector) buildQueries(ctx context.Context, query string) []string {
	queries := []string{query}
	if !i.rewriteEnabled || i.llm == nil {
		return queries
	}
	resp, err := i.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(queryRewriteSystemKey)},
			{Role: domain.RoleUser, Content: query},
		},
	})
	if err != nil {
		return queries
	}
	for _, line := range strings.Split(resp.Message.Content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-*0123456789. "))
		if line == "" || strings.EqualFold(line, query) {
			continue
		}
		queries = append(queries, line)
		if len(queries) >= 3 {
			break
		}
	}
	return queries
}

func interleaveChunks(lists [][]domain.WorkspaceChunk, topK int, seen map[string]struct{}) []domain.WorkspaceChunk {
	var out []domain.WorkspaceChunk
	for pos := 0; len(out) < topK; pos++ {
		progressed := false
		for _, list := range lists {
			if pos >= len(list) {
				continue
			}
			progressed = true
			ch := list[pos]
			key := chunkKey(ch)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ch)
			if len(out) >= topK {
				return out
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

func (i *Injector) InjectContext(ctx context.Context, sessionID uuid.UUID, messages []domain.Message, opts domain.InjectOptions) ([]domain.Message, error) {
	var idx domain.WorkspaceIndex
	var err error
	projectID := registry.ProjectIDFromContext(ctx)
	if projectID != uuid.Nil {

		if branch := registry.BranchFromContext(ctx); branch != "" {
			idx, err = i.store.GetIndexByProjectBranch(ctx, projectID, branch)
			if err != nil || idx.Status != domain.IndexStatusCompleted {
				idx, err = i.store.GetIndexByProject(ctx, projectID)
			}
		} else {
			idx, err = i.store.GetIndexByProject(ctx, projectID)
		}
	} else {
		idx, err = i.store.GetIndexBySession(ctx, sessionID)
	}
	if err != nil {
		return messages, nil
	}
	if idx.Status != domain.IndexStatusCompleted {
		return messages, nil
	}

	query := lastUserMessage(messages)
	if query == "" && opts.TargetSymbol == "" {
		return messages, nil
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = 5
	}

	seen := make(map[string]struct{})
	var chunks []domain.WorkspaceChunk

	if opts.TargetSymbol != "" && opts.ExpandGraph && i.graphCfg.Enabled {
		expanded, err := i.expandGraphChunks(ctx, idx, opts)
		if err != nil {
			return messages, fmt.Errorf("expand graph context: %w", err)
		}
		for _, ch := range expanded {
			key := chunkKey(ch)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chunks = append(chunks, ch)
		}
	}

	if query != "" {
		queries := []string{query}
		if opts.RewriteQuery {
			queries = i.buildQueries(ctx, query)
		}
		var lists [][]domain.WorkspaceChunk
		for _, q := range queries {
			queryEmb, err := i.llm.Embed(ctx, q, i.embeddingModel)
			if err != nil {
				if len(lists) > 0 {
					break
				}
				return messages, fmt.Errorf("embed query: %w", err)
			}
			found, err := i.store.SearchChunksHybrid(ctx, idx.ID, q, queryEmb, topK)
			if err != nil {
				if len(lists) > 0 {
					break
				}
				return messages, fmt.Errorf("search chunks: %w", err)
			}
			lists = append(lists, found)
		}
		for _, ch := range interleaveChunks(lists, topK, seen) {
			chunks = append(chunks, ch)
		}
	}

	if len(chunks) == 0 && !opts.IncludeTree && !opts.IncludeSkeleton {
		return messages, nil
	}

	if opts.MaxChunkTokens > 0 {
		chunks = trimChunksByTokenBudget(chunks, opts.MaxChunkTokens)
	}

	rootPath := idx.RootPath
	if ws := registry.EffectiveWorkspaceDir(ctx); ws != "" {
		rootPath = ws
	}
	skeleton := ""
	if opts.IncludeSkeleton && i.mapper != nil && rootPath != "" {
		skeleton = i.skeleton(ctx, idx, rootPath)
	}
	content := renderInjectMessage(idx, chunks, opts, skeleton)
	if content == "" {
		return messages, nil
	}

	systemMsg := domain.Message{
		Role:    domain.RoleSystem,
		Content: content,
	}
	return append([]domain.Message{systemMsg}, messages...), nil
}

func (i *Injector) expandGraphChunks(ctx context.Context, idx domain.WorkspaceIndex, opts domain.InjectOptions) ([]domain.WorkspaceChunk, error) {
	symbols, err := i.store.SearchSymbols(ctx, opts.TargetSymbol, idx.ID)
	if err != nil {
		return nil, err
	}
	if len(symbols) == 0 {
		return nil, nil
	}

	target := symbols[0]
	if opts.TargetFilePath != "" {
		for _, sym := range symbols {
			if sym.FilePath == opts.TargetFilePath {
				target = sym
				break
			}
		}
	}

	maxChunks := i.graphCfg.MaxExpandedChunks
	if maxChunks <= 0 {
		maxChunks = 8
	}

	targetRef := graph.SymbolRef{
		FilePath:   target.FilePath,
		SymbolName: target.Name,
		Kind:       target.Kind,
	}
	edges, err := i.store.ListEdges(ctx, idx.ID)
	if err != nil {
		return nil, err
	}

	g := buildGraphFromEdges(edges)
	lookup := symbolLookup{symbols: symbols}
	depth := i.graphCfg.MaxExpansionDepth
	if depth <= 0 {
		depth = 2
	}
	refs, _ := graph.ExpandContext(g, lookup, targetRef, target.FilePath, depth, maxChunks)

	seen := make(map[string]struct{})
	var chunks []domain.WorkspaceChunk
	for _, ref := range refs {
		key := ref.Key()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ch, err := i.store.GetChunkBySymbol(ctx, idx.ID, ref.FilePath, ref.SymbolName)
		if err != nil {
			continue
		}
		chunks = append(chunks, ch)
	}

	if len(chunks) == 0 {
		ch, err := i.store.GetChunkBySymbol(ctx, idx.ID, target.FilePath, target.Name)
		if err == nil {
			chunks = append(chunks, ch)
		}
	}
	return chunks, nil
}

type symbolLookup struct {
	symbols []domain.WorkspaceSymbol
}

func (l symbolLookup) Lookup(filePath, symbolName string) (graph.SymbolRef, bool) {
	for _, sym := range l.symbols {
		if sym.FilePath == filePath && sym.Name == symbolName {
			return graph.SymbolRef{
				FilePath:   sym.FilePath,
				SymbolName: sym.Name,
				Kind:       sym.Kind,
			}, true
		}
	}
	for _, sym := range l.symbols {
		if sym.Name == symbolName {
			return graph.SymbolRef{
				FilePath:   sym.FilePath,
				SymbolName: sym.Name,
				Kind:       sym.Kind,
			}, true
		}
	}
	return graph.SymbolRef{}, false
}

type injectChunkView struct {
	Label         string
	Language      string
	ShowSignature bool
	Signature     string
	Content       string
}

type injectMessageInput struct {
	ShowTree     bool
	Tree         string
	ShowSkeleton bool
	Skeleton     string
	Chunks       []injectChunkView
}

var injectMessageKey = prompt.Define("indexer.inject_message", injectMessageInput{
	ShowTree: true, Tree: "project/\n",
})

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func formatInjectMessage(idx domain.WorkspaceIndex, chunks []domain.WorkspaceChunk, opts domain.InjectOptions, mapperSvc *mapper.Service, rootPath string, fanIn map[string]int) string {
	skeleton := ""
	if opts.IncludeSkeleton && mapperSvc != nil && rootPath != "" {
		if built, err := mapperSvc.BuildSkeletonRanked(rootPath, fanIn); err == nil {
			skeleton = built
		}
	}
	return renderInjectMessage(idx, chunks, opts, skeleton)
}

// skeleton is the ranked code skeleton for rootPath, cached per index commit
// (the fan-in ranking) and per state of the tree it was parsed from: parsing
// it is the slow part of an injection. An index with no recorded commit, or a
// root whose state git cannot vouch for, is never cached, since nothing would
// tell a stale entry apart.
func (i *Injector) skeleton(ctx context.Context, idx domain.WorkspaceIndex, rootPath string) string {
	key := ""
	if idx.CommitSHA != "" && i.skeletons != nil {
		if tree, ok := worktreeFingerprint(ctx, rootPath); ok {
			key = idx.ID.String() + "@" + idx.CommitSHA + "|" + rootPath + "@" + tree
			if cached, ok := i.skeletons.get(key); ok {
				return cached
			}
		}
	}
	built, err := i.mapper.BuildSkeletonRanked(rootPath, i.fanIn(ctx, idx))
	if err != nil {
		return ""
	}
	if key != "" {
		i.skeletons.put(key, built)
	}
	return built
}

func (i *Injector) fanIn(ctx context.Context, idx domain.WorkspaceIndex) map[string]int {
	edges, err := i.store.ListEdges(ctx, idx.ID)
	if err != nil || len(edges) == 0 {
		return nil
	}
	fanIn := make(map[string]int, len(edges))
	for _, e := range edges {
		if e.ToFile != "" && e.ToFile != e.FromFile {
			fanIn[e.ToFile]++
		}
	}
	return fanIn
}

func renderInjectMessage(idx domain.WorkspaceIndex, chunks []domain.WorkspaceChunk, opts domain.InjectOptions, skeleton string) string {
	in := injectMessageInput{}

	if opts.IncludeTree && idx.TreeText != "" {
		in.ShowTree = true
		in.Tree = ensureTrailingNewline(idx.TreeText)
	}

	if opts.IncludeSkeleton && skeleton != "" {
		in.ShowSkeleton = true
		in.Skeleton = ensureTrailingNewline(skeleton)
	}

	for _, ch := range chunks {
		label := fmt.Sprintf("%s:%s (lines %d-%d)", ch.FilePath, ch.SymbolName, ch.StartLine, ch.EndLine)
		if ch.SymbolName == "" {
			label = fmt.Sprintf("%s (lines %d-%d)", ch.FilePath, ch.StartLine, ch.EndLine)
		}
		lang := ch.Language
		if lang == "" {
			lang = "text"
		}
		in.Chunks = append(in.Chunks, injectChunkView{
			Label:         label,
			Language:      lang,
			ShowSignature: ch.Signature != "" && !strings.Contains(ch.Content, ch.Signature),
			Signature:     ch.Signature,
			Content:       ensureTrailingNewline(ch.Content),
		})
	}

	return strings.TrimSpace(injectMessageKey.Render(in))
}

func lastUserMessage(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func chunkKey(ch domain.WorkspaceChunk) string {
	return ch.FilePath + ":" + ch.SymbolName + ":" + fmt.Sprintf("%d-%d", ch.StartLine, ch.EndLine)
}

func trimChunksByTokenBudget(chunks []domain.WorkspaceChunk, maxTokens int) []domain.WorkspaceChunk {
	const charsPerToken = 4
	maxChars := maxTokens * charsPerToken
	used := 0
	var result []domain.WorkspaceChunk
	for _, ch := range chunks {
		size := len(ch.Content) + len(ch.Signature)
		if used+size > maxChars && len(result) > 0 {
			break
		}
		result = append(result, ch)
		used += size
	}
	return result
}

func buildGraphFromEdges(edges []domain.WorkspaceEdge) *graph.DependencyGraph {
	g := graph.NewDependencyGraph()
	for _, e := range edges {
		kind := graph.EdgeCall
		if e.EdgeKind == string(graph.EdgeImport) {
			kind = graph.EdgeImport
		}
		g.AddEdge(graph.Edge{
			From: graph.SymbolRef{FilePath: e.FromFile, SymbolName: e.FromSymbol},
			To:   graph.SymbolRef{FilePath: e.ToFile, SymbolName: e.ToSymbol},
			Kind: kind,
		})
	}
	return g
}
