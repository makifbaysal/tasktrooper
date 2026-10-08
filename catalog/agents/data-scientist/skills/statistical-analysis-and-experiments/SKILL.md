---
name: statistical-analysis-and-experiments
category: statistics
description: Use when designing or analysing an A/B test, computing power or sample size, testing whether a difference is real, or writing any statistical claim — effect sizes with intervals, SRM checks, multiple comparisons and causal caveats
tech_stack: Python
source: anthropics/knowledge-work-plugins statistical-analysis (Apache-2.0), adapted; SciPy and statsmodels documentation cited
---
# Statistical Analysis and Experiments

## Overview

A statistical claim is a decision under uncertainty. The analysis has to say how big the effect is, how sure we are, and what could make it wrong — not just whether p < 0.05. The statistics code is production code too: it gets tests.

**Core principle:** decide the design (metric, unit, sample size, stopping rule) before looking at results; report effect size with a confidence interval; treat every causal-sounding sentence as a claim that needs a design behind it.

## 1. Describe before you test

- Report mean **and** median for business metrics; a large gap means skew and the mean alone misleads.
- Use percentiles (p5, p25, p50, p75, p95, p99) and the IQR for skewed data, standard deviation for roughly symmetric data.
- State shape, bounds (a floor at zero, a ceiling at 100%) and how outliers were handled. Never drop outliers silently: classify them as data errors (fix or exclude, with a count), genuine extremes (keep, use robust statistics) or a different population (segment).

## 2. Design an experiment before it runs

Write these down, in the task or your closing message, before analysing anything:

| Item | Example |
|---|---|
| Hypothesis | New checkout raises conversion |
| Primary metric (one) | Orders / visitors, per user |
| Guardrails | Refund rate, page latency |
| Randomisation unit = analysis unit | User (not session, not page view) |
| Minimum detectable effect | +1.0 percentage point on a 10% baseline |
| α, power | 0.05 two-sided, 0.80 |
| Sample size and duration | Computed below; whole weeks to cover weekly cycles |
| Stopping rule | Analyse once at the planned sample size |

```python
from statsmodels.stats.power import NormalIndPower
from statsmodels.stats.proportion import proportion_effectsize

effect = proportion_effectsize(0.11, 0.10)
n_per_arm = NormalIndPower().solve_power(effect_size=effect, alpha=0.05, power=0.8, ratio=1.0)
```

If the traffic cannot reach `n_per_arm` in a reasonable time, say so before the test runs — an underpowered test produces noise and exaggerated "wins".

## 3. Analyse in this order

1. **Sample ratio mismatch first.** Observed assignment counts against the planned split: `scipy.stats.chisquare([n_a, n_b], f_exp=[expected_a, expected_b])`. A very small p-value (below 0.001) means assignment or logging is broken — stop and report that instead of an effect.
2. **The primary metric, once.**
   - Proportions: `proportions_ztest(count=[x_b, x_a], nobs=[n_b, n_a])` and the interval for the difference with `confint_proportions_2indep(x_b, n_b, x_a, n_a)`.
   - Means: Welch's t-test, `scipy.stats.ttest_ind(b, a, equal_var=False)`.
   - Heavy-tailed metrics (revenue): a bootstrap interval for the difference of means, resampling whole users.
   - Ratio metrics where the analysis unit differs from the randomisation unit (revenue per session, randomised by user): delta method or a user-level bootstrap — never a row-level t-test.
3. **Effect size with its interval**, absolute and relative, compared with the MDE. "Significant" but far below the MDE is weak practical evidence; "not significant" with a wide interval is "inconclusive", not "no effect".
4. **Guardrails**, then exploratory segments — labelled as exploratory.

## 4. Multiple comparisons and peeking

- Ten metrics or ten segments at α = 0.05 will produce "findings" by chance. Correct the family: `statsmodels.stats.multitest.multipletests(p_values, alpha=0.05, method="holm")` (or `"fdr_bh"` for many exploratory comparisons), and report how many tests were run.
- Checking the p-value daily and stopping when it dips under 0.05 inflates the false-positive rate far above α. Analyse at the planned sample size; if early stopping is a requirement, it needs a sequential design chosen in advance.

## 5. Causal caveats — say them when they apply

- **Correlation is not causation:** confounding, reverse causation, coincidence. Without randomisation, write "users who use X retain better", not "X improves retention".
- **Simpson's paradox:** an aggregate trend can reverse inside every segment because the mix shifted. Check the key segments before concluding.
- **Survivorship bias:** analysing only entities still present (current customers, surviving stores).
- **Selection bias:** segments defined by the outcome or by self-selection ("users who finished onboarding retain better").
- **Ecological fallacy:** group-level relations applied to individuals.
- **Regression to the mean:** units picked for being extreme drift back without any intervention.
- **Novelty and primacy effects:** early experiment days differ from the steady state.

## 6. Test the statistics code

Hand-computed cases pin the arithmetic; simulation pins the behaviour.

```python
def test_ab_test_false_positive_rate_is_near_alpha() -> None:
    rng = np.random.default_rng(0)
    p_values = np.array([
        two_proportion_test(rng.binomial(1, 0.10, 2_000), rng.binomial(1, 0.10, 2_000)).p_value
        for _ in range(500)
    ])
    assert 0.02 < (p_values < 0.05).mean() < 0.08
```

An A/A simulation like this catches a wrong variance formula, a one-sided test where two-sided was meant, and analysis at the wrong unit. A matching simulation with a true effect of the planned MDE should detect it at roughly the planned power.

## 7. Report format

```
Primary: conversion (per user), B vs A — planned n 14,745/arm, analysed once
A 10.02% (n=15,210)   B 10.71% (n=15,188)   SRM p=0.90
Difference +0.69 pp (95% CI +0.01 to +1.38), relative +6.9%, p=0.048
Planned MDE 1.0 pp: the point estimate is below it — weak practical evidence
Guardrails: refund rate +0.1 pp (CI −0.3 to +0.5), latency unchanged
Exploratory segments (Holm-corrected, 4 tests): none significant
```

Round to the precision the interval supports; "about 7%" is more honest than "6.93%".

## Common Mistakes

- Randomising by user and analysing by session or page view.
- Reporting only the relative lift, with no interval and no baseline rate.
- Calling a non-significant result "no effect".
- Comparing an incomplete current period with a complete previous one.
- Testing twenty segments and reporting the one that "worked".

## Red Flags

- An effect much larger than any previous experiment in this product.
- Unequal arm sizes that were not planned.
- A p-value hovering just under 0.05 after several looks.
- A causal verb ("drives", "causes", "improves") in an observational analysis.
