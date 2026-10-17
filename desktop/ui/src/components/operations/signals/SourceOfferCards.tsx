import { KeyRound, Plug, Store } from "lucide-react";
import { Link } from "react-router-dom";
import type { DataSourceKindInfo, DataSourceOffer, StoreCredentialView } from "@/api";
import { signalLabel } from "@/components/operations/console/signalFormat";
import { credentialReady, offerKey, offerTitleKey } from "@/components/operations/signals/sourceOffers";
import { INTEGRATIONS_PATH } from "@/components/operations/sources/DataSourceFieldInput";
import { kindLabel } from "@/components/operations/sources/dataSourceSchema";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

interface SourceOfferCardsProps {
  offers: DataSourceOffer[];
  kinds: DataSourceKindInfo[];
  /** The store credential vault; null while it is unknown. */
  credentials: StoreCredentialView[] | null;
  /** The offer being connected right now, if any. */
  busyKey?: string;
  onConnect: (offer: DataSourceOffer) => void;
  onDismiss: (offer: DataSourceOffer) => void;
}

/** The data sources a suggest found the app could be read through, had one
 * of that kind existed: "Connect Google Play vitals for this app", one card
 * each, with what it would bind and the credential it needs. */
export function SourceOfferCards({ offers, kinds, credentials, busyKey, onConnect, onDismiss }: SourceOfferCardsProps) {
  const { t } = useI18n();
  const o = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.offers.${key}`, params);
  if (offers.length === 0) return null;
  return (
    <section aria-labelledby="source-offers-title" className="@container space-y-2">
      <h3 id="source-offers-title" className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
        {o("title")}
      </h3>
      <div className="grid gap-2 @3xl:grid-cols-2">
        {offers.map((offer) => {
          const key = offerKey(offer);
          const kind = kinds.find((k) => k.kind === offer.kind);
          const name = kind ? kindLabel(t, kind) : offer.kind;
          const ready = credentialReady(offer, credentials, kind);
          const titleKey = offerTitleKey(offer.kind);
          const params = Object.entries(offer.params ?? {}).filter(([, v]) => v);
          const busy = busyKey === key;
          return (
            <Card key={key} className="flex min-w-0 flex-col gap-3 p-4">
              <div className="flex items-start gap-3">
                <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-info/10 text-info" aria-hidden>
                  <Store className="h-4 w-4" />
                </span>
                <div className="min-w-0 space-y-1">
                  <p className="text-body font-medium">{o(`titles.${titleKey}`, { kind: name })}</p>
                  <p className="text-caption text-muted-foreground">{o(`bodies.${titleKey}`, { kind: name })}</p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-1.5 text-micro">
                {(offer.signals ?? []).map((signal) => (
                  <Badge key={signal} variant="secondary" className="px-2 py-0 text-micro">
                    {signalLabel(t, signal)}
                  </Badge>
                ))}
                {params.map(([k, v]) => (
                  <Badge key={k} variant="outline" className="max-w-56 truncate px-2 py-0 font-mono text-micro font-normal" title={`${k}=${v}`}>
                    {k}={v}
                  </Badge>
                ))}
              </div>
              {ready === false && (
                <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-warning">
                  <KeyRound className="h-3.5 w-3.5" aria-hidden />
                  <span>{o(`credentialMissing.${offer.kind === "app_store_metrics" ? "asc" : "google_play"}`)}</span>
                  <Link to={INTEGRATIONS_PATH} className="font-medium text-info hover:underline">
                    {o("saveCredential")}
                  </Link>
                </p>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" onClick={() => onConnect(offer)} disabled={busy || ready === false}>
                  <Plug aria-hidden />
                  {busy ? o("connecting") : o("connect")}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => onDismiss(offer)} disabled={busy}>
                  {o("notNow")}
                </Button>
                <span className="text-micro text-muted-foreground">{o("recipe", { recipe: offer.recipe_id })}</span>
              </div>
            </Card>
          );
        })}
      </div>
    </section>
  );
}
