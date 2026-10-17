import { signalKindOf } from "@/components/operations/signals/signalBinding";
import { SignalBindingDialog, type SignalBindingDialogProps } from "@/components/operations/signals/SignalBindingDialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useDataSources, useRecipeCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSignals } from "@/hooks/useOpsSignals";
import { signalLabel } from "@/components/operations/console/signalFormat";

type EditorProps = Omit<SignalBindingDialogProps, "sources" | "kinds" | "recipes"> & {
  /** Start from the signal's binding as it is now (read while the editor
   * opens), e.g. to fill in the params a bound recipe is missing. */
  prefillCurrent?: boolean;
};

/** The binding editor for a page that holds no data sources of its own (an
 * entity's Summary): it reads them, and the recipes, while it is open. The
 * unit, when the caller has none yet, is the signal catalog's. */
export function SignalBindingEditor({ prefillCurrent = false, ...props }: EditorProps) {
  const { t } = useI18n();
  const { kinds, sources, loading } = useDataSources();
  const catalog = useRecipeCatalog();
  const current = useComponentSignals(props.componentId, props.env, prefillCurrent);
  if (
    (loading && sources.length === 0) ||
    (catalog.loading && catalog.recipes.length === 0) ||
    (prefillCurrent && current.loading)
  ) {
    return (
      <FormDialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t("operations.signalBinding.title", { signal: signalLabel(t, props.signal) })}
        footer={
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
        }
      >
        <div className="space-y-3" aria-busy="true">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-2/3" />
        </div>
      </FormDialog>
    );
  }
  const spec = catalog.signals.find((s) => s.id === props.signal);
  const unit = props.unit ?? spec?.unit;
  const prefill =
    props.prefill ?? (prefillCurrent ? (current.data?.signals?.find((s) => s.signal === props.signal)?.binding ?? null) : null);
  return (
    <SignalBindingDialog
      {...props}
      unit={unit}
      signalKind={props.signalKind ?? signalKindOf(props.signal, spec?.kind)}
      prefill={prefill}
      sources={sources}
      kinds={kinds}
      recipes={catalog.recipes}
    />
  );
}
