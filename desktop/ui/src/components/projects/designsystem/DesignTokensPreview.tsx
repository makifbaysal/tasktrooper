import { HelpCircle } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import type { DesignTokenTree } from "@/api";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import {
  cssColor,
  cssDimension,
  cssShadow,
  flattenDesignTokens,
  formatTokenValue,
  groupDesignTokens,
  tokenGroup,
  typographySample,
  type DesignToken,
  type DesignTokenCategory,
} from "@/lib/designTokens";
import { cn } from "@/lib/utils";

interface DesignTokensPreviewProps {
  tokens: DesignTokenTree | null | undefined;
  /** Token paths a repository layer overrides; those get a marker. */
  overriddenPaths?: string[];
  className?: string;
}

const SECTION_ORDER: DesignTokenCategory[] = ["color", "typography", "dimension", "radius", "shadow", "other"];

/**
 * A DTCG token tree drawn as what it means: color swatches, a type scale,
 * spacing bars, radius boxes, shadow samples, and a path → value table for
 * everything else. Inline styles carry only each token's own value.
 */
export function DesignTokensPreview({ tokens, overriddenPaths = [], className }: DesignTokensPreviewProps) {
  const { t } = useI18n();
  const groups = useMemo(() => groupDesignTokens(flattenDesignTokens(tokens)), [tokens]);
  const overridden = useMemo(() => new Set(overriddenPaths), [overriddenPaths]);
  const total = SECTION_ORDER.reduce((sum, c) => sum + groups[c].length, 0);

  if (total === 0) {
    return <p className="text-body text-muted-foreground">{t("designSystem.tokens.empty")}</p>;
  }

  return (
    <div className={cn("space-y-section", className)}>
      {SECTION_ORDER.map((category) => {
        const items = groups[category];
        if (items.length === 0) return null;
        return (
          <section key={category} className="space-y-3" aria-label={t(`designSystem.tokens.sections.${category}`)}>
            <div className="flex items-center gap-2">
              <h4 className="text-heading font-semibold">{t(`designSystem.tokens.sections.${category}`)}</h4>
              <Badge variant="secondary">{items.length}</Badge>
            </div>
            {category === "color" && <ColorSection tokens={items} overridden={overridden} />}
            {category === "typography" && <TypographySection tokens={items} overridden={overridden} />}
            {category === "dimension" && <DimensionSection tokens={items} overridden={overridden} />}
            {category === "radius" && <RadiusSection tokens={items} overridden={overridden} />}
            {category === "shadow" && <ShadowSection tokens={items} overridden={overridden} />}
            {category === "other" && <OtherSection tokens={items} overridden={overridden} />}
          </section>
        );
      })}
    </div>
  );
}

interface SectionProps {
  tokens: DesignToken[];
  overridden: Set<string>;
}

function TokenLabel({ token, overridden, leafOnly = false }: { token: DesignToken; overridden: Set<string>; leafOnly?: boolean }) {
  const { t } = useI18n();
  const name = leafOnly ? token.path.slice(token.path.lastIndexOf(".") + 1) : token.path;
  const value = formatTokenValue(token.value);
  const unresolved = token.alias !== undefined && token.resolved === undefined;
  return (
    <div className="min-w-0 space-y-0.5">
      <div className="flex min-w-0 items-center gap-1.5">
        <p className="truncate font-mono text-caption font-medium" title={token.path}>
          {name}
        </p>
        {overridden.has(token.path) && (
          <Badge variant="info" className="shrink-0 px-1.5 py-0 text-micro">
            {t("designSystem.tokens.overridden")}
          </Badge>
        )}
      </div>
      <p className="truncate font-mono text-micro text-muted-foreground" title={value}>
        {value}
      </p>
      {token.alias !== undefined && (
        <p className={cn("truncate text-micro", unresolved ? "text-warning" : "text-muted-foreground")}>
          {unresolved
            ? t("designSystem.tokens.unresolved")
            : `${t("designSystem.tokens.aliasOf", { path: token.alias })} → ${formatTokenValue(token.resolved)}`}
        </p>
      )}
    </div>
  );
}

function TokenRows({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-border rounded-lg border border-border">{children}</div>;
}

function ColorSection({ tokens, overridden }: SectionProps) {
  const byGroup = useMemo(() => {
    const map = new Map<string, DesignToken[]>();
    for (const token of tokens) {
      const group = tokenGroup(token.path);
      map.set(group, [...(map.get(group) ?? []), token]);
    }
    return [...map.entries()];
  }, [tokens]);

  return (
    <div className="space-y-4">
      {byGroup.map(([group, items]) => (
        <div key={group} className="space-y-2">
          {group && <p className="font-mono text-caption text-muted-foreground">{group}</p>}
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6">
            {items.map((token) => {
              const color = cssColor(token.resolved);
              return (
                <div key={token.path} className="min-w-0 space-y-1.5" data-testid="token-swatch">
                  <div
                    role="img"
                    aria-label={`${token.path}: ${formatTokenValue(token.value)}`}
                    className={cn(
                      "flex h-12 items-center justify-center rounded-md border border-border",
                      !color && "bg-muted",
                    )}
                    style={color ? { background: color } : undefined}
                  >
                    {!color && <HelpCircle className="h-4 w-4 text-muted-foreground" aria-hidden />}
                  </div>
                  <TokenLabel token={token} overridden={overridden} leafOnly={!!group} />
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}

function TypographySection({ tokens, overridden }: SectionProps) {
  const { t } = useI18n();
  return (
    <TokenRows>
      {tokens.map((token) => (
        <div
          key={token.path}
          className="grid gap-2 px-4 py-3 sm:grid-cols-[minmax(0,16rem)_minmax(0,1fr)] sm:items-center"
          data-testid="token-type-sample"
        >
          <TokenLabel token={token} overridden={overridden} />
          <p className="truncate text-heading" style={typographySample(token)}>
            {t("designSystem.tokens.sample")}
          </p>
        </div>
      ))}
    </TokenRows>
  );
}

function DimensionSection({ tokens, overridden }: SectionProps) {
  return (
    <TokenRows>
      {tokens.map((token) => {
        const size = cssDimension(token.resolved);
        return (
          <div
            key={token.path}
            className="grid gap-2 px-4 py-3 sm:grid-cols-[minmax(0,16rem)_minmax(0,1fr)] sm:items-center"
            data-testid="token-dimension"
          >
            <TokenLabel token={token} overridden={overridden} />
            {size && <div className="h-2 max-w-full rounded-full bg-primary/70" style={{ width: size }} />}
          </div>
        );
      })}
    </TokenRows>
  );
}

function RadiusSection({ tokens, overridden }: SectionProps) {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
      {tokens.map((token) => {
        const radius = cssDimension(token.resolved);
        return (
          <div key={token.path} className="min-w-0 space-y-1.5" data-testid="token-radius">
            <div
              className="h-12 w-12 border-2 border-primary/60 bg-primary/10"
              style={radius ? { borderRadius: radius } : undefined}
            />
            <TokenLabel token={token} overridden={overridden} />
          </div>
        );
      })}
    </div>
  );
}

function ShadowSection({ tokens, overridden }: SectionProps) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {tokens.map((token) => {
        const shadow = cssShadow(token.resolved);
        return (
          <div key={token.path} className="min-w-0 space-y-2" data-testid="token-shadow">
            <div className="h-16 rounded-lg bg-card" style={shadow ? { boxShadow: shadow } : undefined} />
            <TokenLabel token={token} overridden={overridden} />
          </div>
        );
      })}
    </div>
  );
}

function OtherSection({ tokens, overridden }: SectionProps) {
  const { t } = useI18n();
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-left text-body">
        <thead className="border-b border-border bg-muted/20 text-caption uppercase text-muted-foreground">
          <tr>
            <th className="px-4 py-2 font-medium">{t("designSystem.tokens.columns.token")}</th>
            <th className="px-4 py-2 font-medium">{t("designSystem.tokens.columns.type")}</th>
            <th className="px-4 py-2 font-medium">{t("designSystem.tokens.columns.value")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {tokens.map((token) => (
            <tr key={token.path} data-testid="token-other">
              <td className="px-4 py-2 font-mono text-caption">
                <span className="inline-flex items-center gap-1.5">
                  {token.path}
                  {overridden.has(token.path) && (
                    <Badge variant="info" className="px-1.5 py-0 text-micro">
                      {t("designSystem.tokens.overridden")}
                    </Badge>
                  )}
                </span>
              </td>
              <td className="px-4 py-2 text-caption text-muted-foreground">{token.type ?? "—"}</td>
              <td className="max-w-md break-all px-4 py-2 font-mono text-caption">{formatTokenValue(token.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
