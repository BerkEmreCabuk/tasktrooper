// What a model id tells us about the model, and nothing more.
//
// Providers do not report capabilities on the /models endpoint we use, so this
// reads the name — which is how the ecosystem actually names these things
// (pixtral, -vl, -vision, o1/o3, -thinking). It is a hint for the person
// choosing, never a gate: a wrong guess must not stop anyone selecting a model,
// so nothing here filters the list.
export type ModelCapability = "vision" | "reasoning" | "embedding" | "code";

const VISION = [
  "pixtral",
  "-vl",
  "vl-",
  "vision",
  "llava",
  "gpt-4o",
  "gpt-5",
  "claude-3",
  "claude-4",
  "claude-5",
  "opus",
  "sonnet",
  "haiku",
  "gemini",
  "qwen2-vl",
  "qwen2.5-vl",
  "internvl",
  "moondream",
  "mistral-small",
  "mistral-medium",
  "magistral",
];

// Text-only names that would otherwise match a VISION substring (a Mistral
// "small" that is not the multimodal one, an embedding model with "vl" inside a
// longer word). Checked first.
const NOT_VISION = ["devstral", "codestral", "ministral", "nemo", "embed", "whisper", "tts"];

const REASONING = ["o1", "o3", "o4-mini", "deepseek-r1", "-r1", "thinking", "reasoner", "magistral", "qwq"];
const EMBEDDING = ["embed", "embedding", "bge-", "e5-", "gte-"];
const CODE = ["coder", "codestral", "devstral", "code-"];

function matches(id: string, needles: string[]): boolean {
  return needles.some((n) => id.includes(n));
}

export function modelCapabilities(modelId: string): ModelCapability[] {
  const id = modelId.toLowerCase();
  const caps: ModelCapability[] = [];
  if (matches(id, EMBEDDING)) {
    // An embedding model has no other capability worth listing, and claiming
    // it can "see" would be nonsense.
    return ["embedding"];
  }
  if (!matches(id, NOT_VISION) && matches(id, VISION)) caps.push("vision");
  if (matches(id, REASONING)) caps.push("reasoning");
  if (matches(id, CODE)) caps.push("code");
  return caps;
}

// knownTextOnly reports a model we can positively name as text-only, as opposed
// to one we simply cannot classify. Only the former is worth warning about.
export function knownTextOnly(modelId: string): boolean {
  const id = modelId.toLowerCase();
  return matches(id, NOT_VISION) && !matches(id, EMBEDDING);
}
