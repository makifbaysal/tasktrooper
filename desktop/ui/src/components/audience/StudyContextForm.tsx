import { Plus, X } from "lucide-react";
import type { StudyKind } from "@/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { pagesNeeded, type ContextDraft, type PageDraft } from "@/lib/studyContext";
import { INTENT_PRESETS, MAX_VARIANTS } from "@/lib/studyWizard";
import { cn } from "@/lib/utils";

interface StudyContextFormProps {
  kind: StudyKind;
  value: ContextDraft;
  onChange: (next: ContextDraft) => void;
}

function PageField({ id, label, page, onChange }: { id: string; label: string; page: PageDraft; onChange: (p: PageDraft) => void }) {
  const { t } = useI18n();
  const c = (key: string) => t(`audienceStudy.context.${key}`);
  return (
    <fieldset className="space-y-2">
      <legend className="text-body font-medium">{label}</legend>
      <div className="flex gap-2" role="group" aria-label={label}>
        {(["url", "paste"] as const).map((mode) => (
          <Button key={mode} type="button" size="sm" variant={page.mode === mode ? "default" : "outline"} aria-pressed={page.mode === mode} onClick={() => onChange({ ...page, mode })}>
            {c(mode === "url" ? "pageUrl" : "pagePaste")}
          </Button>
        ))}
      </div>
      {page.mode === "url" ? (
        <div className="space-y-1">
          <Input id={`${id}-url`} aria-label={`${label}: ${c("pageUrl")}`} value={page.url} placeholder="https://" inputMode="url" onChange={(e) => onChange({ ...page, url: e.target.value })} />
          <p className="text-caption text-muted-foreground">{c("pageUrlHelp")}</p>
        </div>
      ) : (
        <Textarea id={`${id}-copy`} aria-label={`${label}: ${c("pagePaste")}`} rows={6} value={page.copy} placeholder={c("pagePastePlaceholder")} onChange={(e) => onChange({ ...page, copy: e.target.value })} />
      )}
    </fieldset>
  );
}

/** The inputs a kind needs besides the goal: prices, a page and an intent, candidate lines or two pages. */
export function StudyContextForm({ kind, value, onChange }: StudyContextFormProps) {
  const { t } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`audienceStudy.context.${key}`, params);
  const set = (patch: Partial<ContextDraft>) => onChange({ ...value, ...patch });
  const setPage = (i: number, page: PageDraft) => set({ pages: i === 0 ? [page, value.pages[1]] : [value.pages[0], page] });

  return (
    <div className="space-y-5">
      {kind === "user_interviews" && (
        <div className="grid grid-cols-[1fr_96px] gap-3">
          <div className="space-y-2">
            <Label htmlFor="study-prices">{c("prices")}</Label>
            <Input id="study-prices" value={value.prices} placeholder="9, 19, 49" inputMode="decimal" onChange={(e) => set({ prices: e.target.value })} />
            <p className="text-caption text-muted-foreground">{c("pricesHelp")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="study-currency">{c("currency")}</Label>
            <Input id="study-currency" value={value.currency} maxLength={3} onChange={(e) => set({ currency: e.target.value })} />
          </div>
        </div>
      )}

      {(kind === "landing_page_test" || kind === "ab_test") && (
        <div className="space-y-2">
          <Label htmlFor="study-intent">{c("intent")}</Label>
          <div className="flex flex-wrap gap-2" role="group" aria-label={c("intentPresets")}>
            {INTENT_PRESETS.map((preset) => {
              const text = c(`intentPreset.${preset}`);
              const active = value.intent.trim().toLowerCase() === text.toLowerCase();
              return (
                <Button key={preset} type="button" size="sm" variant={active ? "default" : "outline"} aria-pressed={active} onClick={() => set({ intent: text })}>
                  {text}
                </Button>
              );
            })}
          </div>
          <Input id="study-intent" value={value.intent} maxLength={200} placeholder={c("intentPlaceholder")} onChange={(e) => set({ intent: e.target.value })} />
          <p className="text-caption text-muted-foreground">{c("intentHelp")}</p>
        </div>
      )}

      {Array.from({ length: pagesNeeded(kind) }, (_, i) => (
        <PageField
          key={i}
          id={`study-page-${i}`}
          label={kind === "ab_test" ? c("pageAB", { label: i === 0 ? "A" : "B" }) : c("page")}
          page={value.pages[i]}
          onChange={(p) => setPage(i, p)}
        />
      ))}

      {kind === "message_testing" && (
        <fieldset className="space-y-2">
          <legend className="text-body font-medium">{c("variants")}</legend>
          <p className="text-caption text-muted-foreground">{c("variantsHelp", { min: 2, max: MAX_VARIANTS })}</p>
          {value.variants.map((v, i) => (
            <div key={i} className="flex gap-2">
              <Input
                aria-label={c("variantN", { n: i + 1 })}
                value={v}
                maxLength={300}
                placeholder={c("variantPlaceholder", { n: i + 1 })}
                onChange={(e) => set({ variants: value.variants.map((x, j) => (j === i ? e.target.value : x)) })}
              />
              {value.variants.length > 2 && (
                <Button type="button" variant="ghost" size="icon" aria-label={c("removeVariant", { n: i + 1 })} onClick={() => set({ variants: value.variants.filter((_, j) => j !== i) })}>
                  <X />
                </Button>
              )}
            </div>
          ))}
          {value.variants.length < MAX_VARIANTS && (
            <Button type="button" variant="outline" size="sm" className={cn("mt-1")} onClick={() => set({ variants: [...value.variants, ""] })}>
              <Plus />
              {c("addVariant")}
            </Button>
          )}
        </fieldset>
      )}
    </div>
  );
}
