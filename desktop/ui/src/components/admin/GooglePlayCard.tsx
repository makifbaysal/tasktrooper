import { type StoreCredentialView } from "@/api";
import { IntegrationCard } from "@/components/admin/IntegrationCard";
import { StoreAppsBrowser } from "@/components/admin/StoreAppPickerDialog";
import { StoreCredentialForm } from "@/components/admin/StoreCredentialsSection";
import { BrandIcon } from "@/components/ui/brand-icon";
import { useI18n } from "@/hooks/useI18n";

interface GooglePlayCardProps {
  /** Undefined while the vault is still loading or could not be read. */
  credential?: StoreCredentialView;
  onChanged: () => void;
}

export function GooglePlayCard({ credential, onChanged }: GooglePlayCardProps) {
  const { t } = useI18n();
  const configured = credential?.configured ?? false;
  return (
    <IntegrationCard
      icon={<BrandIcon brand="googlePlay" className="h-5 w-5" />}
      name={t("settingsPages.integrations.play.title")}
      status={
        configured
          ? { tone: "connected", label: t("settingsPages.integrations.status.connected") }
          : { tone: "idle", label: t("settingsPages.integrations.status.notConnected") }
      }
    >
      <StoreCredentialForm provider="google_play" credential={credential} onChanged={onChanged} collapseWhenSaved hideStatus />
      {configured && <StoreAppsBrowser provider="google_play" configured />}
    </IntegrationCard>
  );
}
