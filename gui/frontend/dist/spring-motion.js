// Spring motion presets are the single source of truth for interaction physics.
// zeta = damping / (2 * sqrt(stiffness * mass)):
//   zeta < 1 underdamped (small, controlled overshoot)
//   zeta = 1 critically damped (fastest settle without bounce)
//   zeta > 1 overdamped (slower convergence, no bounce)
export const SPRING_PRESETS = Object.freeze({
  press: Object.freeze({ stiffness: 1200, damping: 69.282, mass: 1, durationMs: 100 }),
  state: Object.freeze({ stiffness: 800, damping: 56.569, mass: 1, durationMs: 150 }),
  enter: Object.freeze({ stiffness: 500, damping: 37, mass: 1, durationMs: 250 }),
  exit: Object.freeze({ stiffness: 900, damping: 72, mass: 1, durationMs: 150 }),
  settle: Object.freeze({ stiffness: 420, damping: 30, mass: 1, durationMs: 300 })
});

export function dampingRatio(preset = {}) {
  const stiffness = Number(preset.stiffness);
  const damping = Number(preset.damping);
  const mass = Number(preset.mass);
  if (!(stiffness > 0) || !(damping >= 0) || !(mass > 0)) return NaN;
  return damping / (2 * Math.sqrt(stiffness * mass));
}

export function springCharacter(preset) {
  const ratio = dampingRatio(preset);
  if (!Number.isFinite(ratio)) return "invalid";
  if (Math.abs(ratio - 1) < 0.01) return "critical";
  return ratio < 1 ? "underdamped" : "overdamped";
}

// stepResponse evaluates X(0)=0, X'(0)=0 toward target 1.
export function springStepResponse(preset, seconds) {
  const { stiffness, damping, mass } = preset;
  const t = Math.max(0, Number(seconds) || 0);
  if (t === 0) return 0;
  const omega = Math.sqrt(stiffness / mass);
  const zeta = dampingRatio(preset);
  if (zeta < 1) {
    const decay = Math.exp(-zeta * omega * t);
    const damped = omega * Math.sqrt(1 - zeta * zeta);
    return 1 - decay * (Math.cos(damped * t) + (zeta * omega / damped) * Math.sin(damped * t));
  }
  if (Math.abs(zeta - 1) < 1e-6) {
    return 1 - Math.exp(-omega * t) * (1 + omega * t);
  }
  const root = omega * Math.sqrt(zeta * zeta - 1);
  const r1 = -zeta * omega + root;
  const r2 = -zeta * omega - root;
  const a = -r2 / (r1 - r2);
  const b = r1 / (r1 - r2);
  return 1 - a * Math.exp(r1 * t) - b * Math.exp(r2 * t);
}

// springEasing converts the physical response into a CSS linear() easing. The
// endpoint is pinned to 1 so Web Animations always lands on the exact final state.
export function springEasing(preset, samples = 36) {
  const count = Math.max(8, Math.trunc(samples));
  const duration = Math.max(1, Number(preset.durationMs) || 1) / 1000;
  const points = [];
  for (let index = 0; index <= count; index += 1) {
    const percent = (index / count) * 100;
    const value = index === count ? 1 : springStepResponse(preset, duration * index / count);
    points.push(`${Number(value.toFixed(4))} ${Number(percent.toFixed(2))}%`);
  }
  return `linear(${points.join(", ")})`;
}

export function animateSpring(element, preset, keyframes, options = {}) {
  if (!element?.animate) return null;
  const physics = typeof preset === "string" ? SPRING_PRESETS[preset] : preset;
  if (!physics) return null;
  return element.animate(keyframes, {
    duration: physics.durationMs,
    easing: springEasing(physics),
    fill: options.fill || "both",
    composite: options.composite || "replace"
  });
}
