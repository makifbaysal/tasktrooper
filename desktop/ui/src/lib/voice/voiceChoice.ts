import type {
  DesktopVoiceCapabilities,
  DesktopVoiceChoice,
  DesktopVoiceEngineOption,
  DesktopVoiceLanguage,
} from "@/lib/desktop-bridge";

/**
 * The stored pick for `lang`, kept only as far as this computer still has it:
 * a model that is gone falls back to the default, a voice that is gone to the
 * model's default voice.
 */
export function effectiveVoice(
  caps: DesktopVoiceCapabilities | null,
  lang: DesktopVoiceLanguage,
  stored: DesktopVoiceChoice | undefined,
): { option: DesktopVoiceEngineOption | null; choice: DesktopVoiceChoice | undefined } {
  const options = caps?.speakEngines?.[lang] ?? [];
  const option = options.find((o) => o.engine === stored?.engine) ?? options[0] ?? null;
  if (!option) return { option: null, choice: undefined };
  const style = stored?.style && option.styles.includes(stored.style) ? stored.style : option.styles[0];
  return { option, choice: { engine: option.engine, ...(style ? { style } : {}) } };
}
