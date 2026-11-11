import { Loader2, Plus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Audience, type AudienceSegment, type Repository } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

const NO_REPOSITORY = "__none__";
const MAX_SEGMENTS = 8;

interface AudienceFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The audience whose brief is edited, or null to write a new one. */
  audience?: Audience | null;
  /** The project's own words to start from, e.g. when opened from a repository. */
  onSaved: (audience: Audience) => void;
}

/**
 * The brief an audience is written from: what the product is and how it stands
 * today, who it is for and what they use instead. Saving a brief writes nothing
 * and spends nothing; the people are written when the person builds.
 */
export function AudienceFormDialog({ open, onOpenChange, audience = null, onSaved }: AudienceFormDialogProps) {
  const { t } = useI18n();
  const b = (key: string, params?: Record<string, string | number>) => t(`audienceHub.brief.${key}`, params);
  const [name, setName] = useState("");
  const [repository, setRepository] = useState(NO_REPOSITORY);
  const [product, setProduct] = useState("");
  const [segments, setSegments] = useState<AudienceSegment[]>([]);
  const [locations, setLocations] = useState("");
  const [currentSolutions, setCurrentSolutions] = useState("");
  const [pricing, setPricing] = useState("");
  const [topic, setTopic] = useState("");
  const [language, setLanguage] = useState("");
  const [count, setCount] = useState("12");
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    const brief = audience?.brief;
    setName(audience?.name ?? "");
    setRepository(audience?.repository_id ?? NO_REPOSITORY);
    setProduct(brief?.product ?? "");
    setSegments(brief?.segments?.map((s) => ({ ...s })) ?? []);
    setLocations(brief?.locations ?? "");
    setCurrentSolutions(brief?.current_solutions ?? "");
    setPricing(brief?.pricing ?? "");
    setTopic(brief?.topic ?? "");
    setLanguage(brief?.language ?? "");
    setCount(String(audience?.persona_target ?? 12));
    api
      .listRepositories()
      .then((out) => setRepositories(out.repositories ?? []))
      .catch(() => setRepositories([]));
  }, [open, audience]);

  const setSegment = (i: number, patch: Partial<AudienceSegment>) =>
    setSegments((prev) => prev.map((s, j) => (j === i ? { ...s, ...patch } : s)));

  const valid = name.trim() !== "" && product.trim() !== "";

  const save = async () => {
    setSaving(true);
    const input = {
      name: name.trim(),
      repository: repository === NO_REPOSITORY ? "" : repository,
      persona_target: Math.min(40, Math.max(1, Number.parseInt(count, 10) || 12)),
      brief: {
        product: product.trim(),
        segments: segments.filter((s) => s.name.trim() !== "").map((s) => ({ ...s, name: s.name.trim() })),
        locations: locations.trim(),
        current_solutions: currentSolutions.trim(),
        pricing: pricing.trim(),
        topic: topic.trim(),
        language: language.trim(),
      },
    };
    try {
      const saved = audience ? await api.updateAudience(audience.id, input) : await api.createAudience(input);
      toast.success(audience ? b("updated") : b("created"));
      onOpenChange(false);
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={audience ? b("editTitle") : b("newTitle")}
      description={b("intro")}
      className="sm:max-w-2xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving || !valid}>
            {saving && <Loader2 className="animate-spin" />}
            {audience ? t("common.save") : b("create")}
          </Button>
        </>
      }
    >
      <div className="max-h-[60vh] space-y-4 overflow-y-auto pr-1">
        <div className="space-y-2">
          <Label htmlFor="aud-name">{b("name")}</Label>
          <Input id="aud-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={b("namePlaceholder")} autoFocus />
        </div>
        <div className="space-y-2">
          <Label htmlFor="aud-repo">{b("repository")}</Label>
          <Select value={repository} onValueChange={setRepository}>
            <SelectTrigger id="aud-repo">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_REPOSITORY}>{b("noRepository")}</SelectItem>
              {repositories.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-caption text-muted-foreground">{b("repositoryHelp")}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="aud-product">{b("product")}</Label>
          <Textarea id="aud-product" rows={5} value={product} onChange={(e) => setProduct(e.target.value)} placeholder={b("productPlaceholder")} />
          <p className="text-caption text-muted-foreground">{b("productHelp")}</p>
        </div>

        <fieldset className="space-y-2">
          <legend className="text-body font-medium">{b("segments")}</legend>
          <p className="text-caption text-muted-foreground">{b("segmentsHelp")}</p>
          {segments.map((s, i) => (
            <div key={i} className="grid grid-cols-[1fr_72px_auto] items-start gap-2 sm:grid-cols-[180px_1fr_72px_auto]">
              <Input aria-label={b("segmentName")} placeholder={b("segmentName")} value={s.name} onChange={(e) => setSegment(i, { name: e.target.value })} />
              <Input
                className="col-span-3 row-start-2 sm:col-span-1 sm:row-start-auto"
                aria-label={b("segmentDescription")}
                placeholder={b("segmentDescription")}
                value={s.description ?? ""}
                onChange={(e) => setSegment(i, { description: e.target.value })}
              />
              <Input
                type="number"
                min={0}
                aria-label={b("segmentCount")}
                placeholder={b("segmentCount")}
                value={s.count ?? ""}
                onChange={(e) => setSegment(i, { count: e.target.value === "" ? 0 : Number(e.target.value) })}
              />
              <Button variant="ghost" size="icon" onClick={() => setSegments((prev) => prev.filter((_, j) => j !== i))} aria-label={b("removeSegment")}>
                <X />
              </Button>
            </div>
          ))}
          {segments.length < MAX_SEGMENTS && (
            <Button variant="outline" size="sm" onClick={() => setSegments((prev) => [...prev, { name: "", description: "", count: 0 }])}>
              <Plus />
              {b("addSegment")}
            </Button>
          )}
        </fieldset>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="aud-locations">{b("locations")}</Label>
            <Input id="aud-locations" value={locations} onChange={(e) => setLocations(e.target.value)} placeholder={b("locationsPlaceholder")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="aud-pricing">{b("pricing")}</Label>
            <Input id="aud-pricing" value={pricing} onChange={(e) => setPricing(e.target.value)} placeholder={b("pricingPlaceholder")} />
          </div>
        </div>
        <div className="space-y-2">
          <Label htmlFor="aud-solutions">{b("currentSolutions")}</Label>
          <Input id="aud-solutions" value={currentSolutions} onChange={(e) => setCurrentSolutions(e.target.value)} placeholder={b("currentSolutionsPlaceholder")} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="aud-topic">{b("topic")}</Label>
          <Input id="aud-topic" value={topic} onChange={(e) => setTopic(e.target.value)} placeholder={b("topicPlaceholder")} />
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="aud-language">{b("language")}</Label>
            <Input id="aud-language" value={language} onChange={(e) => setLanguage(e.target.value)} placeholder={b("languagePlaceholder")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="aud-count">{b("count")}</Label>
            <Input id="aud-count" type="number" min={1} max={40} value={count} onChange={(e) => setCount(e.target.value)} />
            <p className="text-caption text-muted-foreground">{b("countHelp")}</p>
          </div>
        </div>
      </div>
    </FormDialog>
  );
}
