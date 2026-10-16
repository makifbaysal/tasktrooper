import { SignalBindingDialog, type SignalBindingDialogProps } from "@/components/operations/signals/SignalBindingDialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useDataSources, useRecipeCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { signalLabel } from "@/components/operations/console/signalFormat";

type EditorProps = Omit<SignalBindingDialogProps, "sources" | "kinds" | "recipes">;

/** The binding editor for a page that holds no data sources of its own (an
 * entity's Summary): it reads them, and the recipes, while it is open. */
export function SignalBindingEditor(props: EditorProps) {
  const { t } = useI18n();
  const { kinds, sources, loading } = useDataSources();
  const catalog = useRecipeCatalog();
  if ((loading && sources.length === 0) || (catalog.loading && catalog.recipes.length === 0)) {
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
  return <SignalBindingDialog {...props} sources={sources} kinds={kinds} recipes={catalog.recipes} />;
}
