import { FlaskConical, Loader2, Plus, Sparkles, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, MAX_SMOKE_CHECKS, MAX_SMOKE_LATENCY_MS, type SmokeCheck, type SmokeGenerationJob, type SmokeResult } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDurationMs } from "@/lib/utils";

const GENERATION_POLL_MS = 2000;

const SMOKE_METHODS = ["GET", "HEAD"] as const;

// Mirrors the server's SmokeCheck.Name limit (see server contract).
export const MAX_SMOKE_NAME_LEN = 80;

// Fixed seed data, not run through t(): "Ana sayfa" names the one preset with
// no universal endpoint convention, "Health"/"API health" match how those
// endpoints are actually named in code regardless of UI language.
const PRESETS: { key: string; check: SmokeCheck }[] = [
  { key: "blank", check: { method: "GET", path: "/" } },
  { key: "home", check: { name: "Ana sayfa", method: "GET", path: "/", expect_status: 200 } },
  { key: "health", check: { name: "Health", method: "GET", path: "/health", expect_status: 200 } },
  { key: "apiHealth", check: { name: "API health", method: "GET", path: "/api/health", expect_status: 200 } },
];

function isAbsolutePath(path: string): boolean {
  return /^https?:\/\//i.test(path.trim());
}

function resolvedUrl(path: string, baseUrl?: string): string | null {
  const p = path.trim();
  if (!p) return null;
  if (isAbsolutePath(p)) return p;
  if (!baseUrl) return null;
  const base = baseUrl.replace(/\/+$/, "");
  const suffix = p.startsWith("/") ? p : `/${p}`;
  return `${base}${suffix}`;
}

interface FieldErrors {
  name?: string;
  path?: string;
  expect_status?: string;
  max_latency_ms?: string;
}

function fieldErrors(check: SmokeCheck, t: (key: string, params?: Record<string, string | number>) => string): FieldErrors {
  const errors: FieldErrors = {};
  const path = check.path.trim();
  if (!path || (!isAbsolutePath(path) && !path.startsWith("/"))) {
    errors.path = t("release.deliveryEdit.errors.smokePath");
  }
  if (check.expect_status !== undefined && (check.expect_status < 100 || check.expect_status > 599)) {
    errors.expect_status = t("release.deliveryEdit.errors.smokeExpectStatus");
  }
  if ((check.name?.length ?? 0) > MAX_SMOKE_NAME_LEN) {
    errors.name = t("release.deliveryEdit.errors.smokeName", { max: MAX_SMOKE_NAME_LEN });
  }
  if (check.max_latency_ms !== undefined && (check.max_latency_ms < 1 || check.max_latency_ms > MAX_SMOKE_LATENCY_MS)) {
    errors.max_latency_ms = t("release.deliveryEdit.errors.smokeLatency", { max: MAX_SMOKE_LATENCY_MS });
  }
  return errors;
}

interface SmokeChecksEditorProps {
  checks: SmokeCheck[];
  onChange: (checks: SmokeCheck[]) => void;
  componentId: string;
  /** The production environment's URL, when bound — a relative path resolves against it. */
  baseUrl?: string;
}

/**
 * One block per smoke check, every field labeled, plus a "test now" run
 * against production that needs nothing saved first — the whole reason the
 * old bare row of unlabeled inputs had to go was that a saved mistake here
 * only ever surfaced after the next real deploy.
 */
export function SmokeChecksEditor({ checks, onChange, componentId, baseUrl }: SmokeChecksEditorProps) {
  const { t } = useI18n();
  const [results, setResults] = useState<(SmokeResult | undefined)[] | null>(null);
  const [baseUrlUsed, setBaseUrlUsed] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  // Which check blocks came from the AI, by their current position — not
  // identity, because editing a check replaces its object but must keep the
  // badge; removeCheck below re-indexes this set so it survives a deletion.
  const [suggestedIndices, setSuggestedIndices] = useState<Set<number>>(new Set());
  const [job, setJob] = useState<SmokeGenerationJob | null>(null);
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // The poll interval and the unmount cleanup close over these once; refs
  // keep them reading the latest props/state instead of what existed when
  // the generation was kicked off two minutes ago.
  const checksRef = useRef(checks);
  const onChangeRef = useRef(onChange);
  const jobRef = useRef(job);
  useEffect(() => {
    checksRef.current = checks;
    onChangeRef.current = onChange;
    jobRef.current = job;
  });

  const mutate = (next: SmokeCheck[]) => {
    setResults(null);
    setTestError(null);
    onChange(next);
  };

  const addPreset = (preset: SmokeCheck) => {
    if (checks.length >= MAX_SMOKE_CHECKS) return;
    mutate([...checks, { ...preset }]);
  };

  const updateCheck = (index: number, patch: Partial<SmokeCheck>) =>
    mutate(checks.map((c, i) => (i === index ? { ...c, ...patch } : c)));

  const setMethod = (index: number, method: string) =>
    updateCheck(index, { method, contains: method === "HEAD" ? undefined : checks[index].contains });

  const removeCheck = (index: number) => {
    mutate(checks.filter((_, i) => i !== index));
    setSuggestedIndices((prev) => {
      const next = new Set<number>();
      prev.forEach((i) => {
        if (i < index) next.add(i);
        else if (i > index) next.add(i - 1);
      });
      return next;
    });
  };

  const canTest = checks.length > 0 && checks.every((c) => c.path.trim().length > 0);

  const runTest = async () => {
    setTesting(true);
    setTestError(null);
    try {
      const res = await api.testSmokeChecks(componentId, checks);
      setResults(res.results);
      setBaseUrlUsed(res.base_url);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("release.deliveryEdit.smoke.test.failed");
      setTestError(message);
      toast.error(message);
    } finally {
      setTesting(false);
    }
  };

  const applyGeneratedChecks = (finished: SmokeGenerationJob) => {
    const startIndex = checksRef.current.length;
    onChangeRef.current([...checksRef.current, ...finished.checks]);
    setResults((prev) => {
      const base: (SmokeResult | undefined)[] = prev ? [...prev] : [];
      while (base.length < startIndex) base.push(undefined);
      return [...base, ...finished.results];
    });
    setSuggestedIndices((prev) => {
      const next = new Set(prev);
      finished.checks.forEach((_, i) => next.add(startIndex + i));
      return next;
    });
    const suggested = t("release.deliveryEdit.smoke.ai.toastSuggested", { count: finished.checks.length });
    const dropped =
      finished.dropped > 0 ? t("release.deliveryEdit.smoke.ai.toastDropped", { count: finished.dropped }) : null;
    toast.success(dropped ? `${suggested} · ${dropped}` : suggested);
  };

  const handleGenerationUpdate = (fresh: SmokeGenerationJob) => {
    if (fresh.status === "cancelled") {
      setJob(null);
      return;
    }
    if (fresh.status === "done" && fresh.checks.length > 0) {
      applyGeneratedChecks(fresh);
      setJob(null);
      return;
    }
    setJob(fresh);
  };

  useEffect(() => {
    if (!job || job.status !== "running") return;
    const jobId = job.job_id;
    const id = window.setInterval(() => {
      api
        .getSmokeGeneration(jobId)
        .then(handleGenerationUpdate)
        .catch(() => setJob(null));
    }, GENERATION_POLL_MS);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job?.job_id, job?.status]);

  useEffect(() => {
    if (job?.status !== "running") return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [job?.status]);

  useEffect(() => {
    return () => {
      if (jobRef.current?.status === "running") {
        void api.cancelSmokeGeneration(jobRef.current.job_id).catch(() => undefined);
      }
    };
  }, []);

  const generating = starting || job?.status === "running";
  const canGenerate = !generating && checks.length < MAX_SMOKE_CHECKS;

  const startGeneration = async () => {
    if (!canGenerate) return;
    setStarting(true);
    setStartError(null);
    try {
      const started = await api.generateSmokeChecks(componentId, checks);
      handleGenerationUpdate(started);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("release.deliveryEdit.smoke.ai.startFailedTitle");
      setStartError(message);
      toast.error(message);
    } finally {
      setStarting(false);
    }
  };

  const stopGeneration = async () => {
    if (!job) return;
    const jobId = job.job_id;
    setJob(null);
    try {
      await api.cancelSmokeGeneration(jobId);
    } catch {
      // Best-effort — the run stops mattering to this editor either way.
    }
  };

  const generateButton = (variant: "outline" | "default" = "outline") => (
    <Button type="button" size="sm" variant={variant} onClick={() => void startGeneration()} disabled={!canGenerate}>
      {generating ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <Sparkles className="mr-1 h-3.5 w-3.5" />}
      {t("release.deliveryEdit.smoke.ai.generate")}
    </Button>
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <Label>{t("release.deliveryEdit.smoke.title")}</Label>
          <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.description")}</p>
        </div>
        <div className="flex items-center gap-2">
          {generateButton()}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" size="sm" variant="outline" disabled={checks.length >= MAX_SMOKE_CHECKS}>
                <Plus className="mr-1 h-3.5 w-3.5" />
                {t("release.deliveryEdit.smoke.add")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              {PRESETS.map((preset) => (
                <DropdownMenuItem key={preset.key} onSelect={() => addPreset(preset.check)}>
                  {t(`release.deliveryEdit.smoke.presets.${preset.key}`)}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {job?.status === "running" && (
        <Notice variant="info" title={t("release.deliveryEdit.smoke.ai.runningTitle")}>
          <p>
            {t("release.deliveryEdit.smoke.ai.runningDetail", {
              agent: job.agent_name,
              elapsed: formatDurationMs(now - new Date(job.started_at).getTime()),
            })}
          </p>
          <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => void stopGeneration()}>
            {t("release.deliveryEdit.smoke.ai.stop")}
          </Button>
        </Notice>
      )}

      {job?.status === "done" && job.checks.length === 0 && (
        <Notice variant="warning" title={t("release.deliveryEdit.smoke.ai.emptyTitle")}>
          <p>{job.error}</p>
        </Notice>
      )}

      {job?.status === "failed" && (
        <Notice variant="error" title={t("release.deliveryEdit.smoke.ai.failedTitle")}>
          <p>{job.error}</p>
          <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => void startGeneration()}>
            {t("release.deliveryEdit.smoke.ai.retry")}
          </Button>
        </Notice>
      )}

      {startError && (
        <Notice variant="error" title={t("release.deliveryEdit.smoke.ai.startFailedTitle")}>
          <p>{startError}</p>
        </Notice>
      )}

      {checks.length === 0 ? (
        <div className="space-y-3">
          <p className="text-caption text-muted-foreground">{t("release.deliveryEdit.smoke.empty")}</p>
          {generateButton("default")}
        </div>
      ) : (
        <div className="space-y-3">
          {checks.map((check, index) => {
            const errors = fieldErrors(check, t);
            const isHead = check.method === "HEAD";
            const resolved = resolvedUrl(check.path, baseUrl);
            const showPrefix = Boolean(baseUrl) && check.path.trim().length > 0 && !isAbsolutePath(check.path);
            const result = results?.[index];
            const label = check.name?.trim() || `${check.method ?? "GET"} ${check.path || "/"}`;

            return (
              <div key={index} className="space-y-3 rounded-lg border border-border p-3">
                <div className="flex items-center gap-2">
                  <span className="shrink-0 text-caption font-medium text-muted-foreground">#{index + 1}</span>
                  <span className="min-w-0 flex-1 truncate text-body font-medium">{label}</span>
                  {suggestedIndices.has(index) && (
                    <Badge variant="info" className="shrink-0">
                      {t("release.deliveryEdit.smoke.ai.suggestedBadge")}
                    </Badge>
                  )}
                  {result && (
                    <Badge variant={result.ok ? "success" : "destructive"} className="shrink-0">
                      {result.ok
                        ? result.latency_ms !== undefined
                          ? t("release.deliveryEdit.smoke.test.passBadge", { status: result.status ?? "", latency: result.latency_ms })
                          : t("release.deliveryEdit.smoke.test.passBadgeNoLatency", { status: result.status ?? "" })
                        : result.status !== undefined
                          ? t("release.deliveryEdit.smoke.test.failBadge", { status: result.status })
                          : t("release.deliveryEdit.smoke.test.failBadgeNoStatus")}
                    </Badge>
                  )}
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="h-8 w-8 shrink-0"
                    onClick={() => removeCheck(index)}
                    aria-label={t("release.deliveryEdit.smoke.remove")}
                  >
                    <X className="h-3.5 w-3.5" />
                  </Button>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1">
                    <Label htmlFor={`smoke-name-${index}`}>{t("release.deliveryEdit.smoke.fields.name")}</Label>
                    <Input
                      id={`smoke-name-${index}`}
                      value={check.name ?? ""}
                      onChange={(e) => updateCheck(index, { name: e.target.value || undefined })}
                      aria-invalid={Boolean(errors.name)}
                    />
                    <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.fields.nameHelp")}</p>
                    {errors.name && <p className="text-micro text-destructive">{errors.name}</p>}
                  </div>

                  <div className="space-y-1">
                    <Label htmlFor={`smoke-method-${index}`}>{t("release.deliveryEdit.smoke.fields.method")}</Label>
                    <Select value={check.method ?? "GET"} onValueChange={(v) => setMethod(index, v)}>
                      <SelectTrigger id={`smoke-method-${index}`}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {SMOKE_METHODS.map((m) => (
                          <SelectItem key={m} value={m}>
                            {m}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.fields.methodHelp")}</p>
                  </div>

                  <div className="space-y-1 sm:col-span-2">
                    <Label htmlFor={`smoke-path-${index}`}>{t("release.deliveryEdit.smoke.fields.path")}</Label>
                    {showPrefix ? (
                      <div className="flex items-center rounded-md border border-input shadow-[var(--shadow-raised)] focus-within:ring-2 focus-within:ring-ring">
                        <span className="max-w-[45%] shrink-0 truncate rounded-l-md border-r border-input bg-muted px-2 py-1 font-mono text-caption text-muted-foreground">
                          {baseUrl}
                        </span>
                        <Input
                          id={`smoke-path-${index}`}
                          value={check.path}
                          onChange={(e) => updateCheck(index, { path: e.target.value })}
                          placeholder={t("release.deliveryEdit.smoke.fields.pathPlaceholder")}
                          className="border-0 font-mono shadow-none focus-visible:ring-0"
                          aria-invalid={Boolean(errors.path)}
                        />
                      </div>
                    ) : (
                      <Input
                        id={`smoke-path-${index}`}
                        value={check.path}
                        onChange={(e) => updateCheck(index, { path: e.target.value })}
                        placeholder={t("release.deliveryEdit.smoke.fields.pathPlaceholder")}
                        className="font-mono"
                        aria-invalid={Boolean(errors.path)}
                      />
                    )}
                    <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.fields.pathHelp")}</p>
                    {resolved ? (
                      <p className="truncate font-mono text-micro text-muted-foreground">{resolved}</p>
                    ) : !isAbsolutePath(check.path) && !baseUrl ? (
                      <p className="text-micro text-warning">{t("release.deliveryEdit.smoke.fields.resolvedUrlUnbound")}</p>
                    ) : null}
                    {errors.path && <p className="text-micro text-destructive">{errors.path}</p>}
                  </div>

                  <div className="space-y-1">
                    <Label htmlFor={`smoke-status-${index}`}>{t("release.deliveryEdit.smoke.fields.expectStatus")}</Label>
                    <Input
                      id={`smoke-status-${index}`}
                      type="number"
                      min={100}
                      max={599}
                      value={check.expect_status ?? ""}
                      onChange={(e) => updateCheck(index, { expect_status: e.target.value === "" ? undefined : Number(e.target.value) })}
                      placeholder={t("release.deliveryEdit.smoke.fields.expectStatusPlaceholder")}
                      aria-invalid={Boolean(errors.expect_status)}
                    />
                    {errors.expect_status && <p className="text-micro text-destructive">{errors.expect_status}</p>}
                  </div>

                  <div className="space-y-1">
                    <Label htmlFor={`smoke-contains-${index}`}>{t("release.deliveryEdit.smoke.fields.contains")}</Label>
                    <Input
                      id={`smoke-contains-${index}`}
                      value={check.contains ?? ""}
                      disabled={isHead}
                      onChange={(e) => updateCheck(index, { contains: e.target.value || undefined })}
                      placeholder={t("release.deliveryEdit.smoke.fields.containsPlaceholder")}
                    />
                    <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.fields.containsHelp")}</p>
                  </div>

                  <div className="space-y-1">
                    <Label htmlFor={`smoke-latency-${index}`}>{t("release.deliveryEdit.smoke.fields.maxLatency")}</Label>
                    <Input
                      id={`smoke-latency-${index}`}
                      type="number"
                      min={1}
                      max={MAX_SMOKE_LATENCY_MS}
                      value={check.max_latency_ms ?? ""}
                      onChange={(e) => updateCheck(index, { max_latency_ms: e.target.value === "" ? undefined : Number(e.target.value) })}
                      placeholder={t("release.deliveryEdit.smoke.fields.maxLatencyPlaceholder")}
                      aria-invalid={Boolean(errors.max_latency_ms)}
                    />
                    <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.fields.maxLatencyHelp")}</p>
                    {errors.max_latency_ms && <p className="text-micro text-destructive">{errors.max_latency_ms}</p>}
                  </div>
                </div>

                {result?.error && <p className="text-caption text-destructive">{result.error}</p>}
              </div>
            );
          })}
        </div>
      )}

      {checks.length >= MAX_SMOKE_CHECKS && (
        <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.smoke.maxReached", { max: MAX_SMOKE_CHECKS })}</p>
      )}

      {checks.length > 0 && (
        <div className="space-y-2 rounded-md border border-border p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Button type="button" size="sm" variant="outline" onClick={() => void runTest()} disabled={testing || !canTest}>
              {testing ? (
                <Loader2 className={cn("mr-1 h-3.5 w-3.5 animate-spin")} />
              ) : (
                <FlaskConical className="mr-1 h-3.5 w-3.5" />
              )}
              {testing ? t("release.deliveryEdit.smoke.test.running") : t("release.deliveryEdit.smoke.test.run")}
            </Button>
            {results && (
              <span className="text-caption text-muted-foreground">
                {t("release.deliveryEdit.smoke.test.summary", { passed: results.filter((r) => r?.ok).length, total: results.length })}
                {baseUrlUsed && ` · ${t("release.deliveryEdit.smoke.test.baseUrl", { url: baseUrlUsed })}`}
              </span>
            )}
          </div>
          {testError && (
            <Notice variant="error" title={t("release.deliveryEdit.smoke.test.failed")}>
              <p>{testError}</p>
            </Notice>
          )}
        </div>
      )}
    </div>
  );
}
