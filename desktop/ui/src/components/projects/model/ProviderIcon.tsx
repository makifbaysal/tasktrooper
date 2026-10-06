import type { CloudProviderKind } from "@/api";
import { BrandIcon, type Brand } from "@/components/ui/brand-icon";

const PROVIDER_BRANDS: Record<CloudProviderKind, Brand> = {
  vercel: "vercel",
  gcp: "googleCloud",
  aws: "aws",
};

interface ProviderIconProps {
  provider: CloudProviderKind;
  className?: string;
}

export function ProviderIcon({ provider, className }: ProviderIconProps) {
  return <BrandIcon brand={PROVIDER_BRANDS[provider] ?? "vercel"} className={className} />;
}
