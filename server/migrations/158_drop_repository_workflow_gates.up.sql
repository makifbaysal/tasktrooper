-- Repository-level workflow gates are removed from the product: the review
-- chain is now always enforced by workflow stages, the pipeline gate is
-- unconditional (bounded by its own timeout), release is never gated on a
-- recorded deploy, and release always fires from done. Coverage/mutation
-- stay as repository columns but become a read-only projection of the
-- single component's gates (application/projectmodel), so they are seeded
-- here from whatever the user had set before the columns they rode on go
-- away.

-- a. Seed component gates from the repo-level values so nothing the user set
--    is lost. Only fills a key the component has no opinion on yet, and only
--    when the repository's value is non-default.
UPDATE project_components pc
SET gates = pc.gates
    || CASE WHEN NOT (pc.gates ? 'coverage_enabled') AND r.require_overall_coverage
            THEN jsonb_build_object('coverage_enabled', true) ELSE '{}'::jsonb END
    || CASE WHEN NOT (pc.gates ? 'coverage_threshold') AND r.coverage_threshold > 0
            THEN jsonb_build_object('coverage_threshold', r.coverage_threshold) ELSE '{}'::jsonb END
    || CASE WHEN NOT (pc.gates ? 'mutation_enabled') AND r.mutation_enabled
            THEN jsonb_build_object('mutation_enabled', true) ELSE '{}'::jsonb END
    || CASE WHEN NOT (pc.gates ? 'mutation_threshold') AND r.mutation_threshold > 0
            THEN jsonb_build_object('mutation_threshold', r.mutation_threshold) ELSE '{}'::jsonb END
FROM repositories r
WHERE r.id = pc.repository_id
  AND (
    (NOT (pc.gates ? 'coverage_enabled') AND r.require_overall_coverage) OR
    (NOT (pc.gates ? 'coverage_threshold') AND r.coverage_threshold > 0) OR
    (NOT (pc.gates ? 'mutation_enabled') AND r.mutation_enabled) OR
    (NOT (pc.gates ? 'mutation_threshold') AND r.mutation_threshold > 0)
  );

-- b. require_release_deploy is dropped from the behaviour registry: no stage
--    can carry it any more, following the exact pattern of migration 149.
UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' <> 'require_release_deploy')
    , '[]'::jsonb
)
WHERE behaviours @> '[{"key":"require_release_deploy"}]';

-- c. The gates themselves.
ALTER TABLE repositories
    DROP COLUMN IF EXISTS auto_release_on_done,
    DROP COLUMN IF EXISTS require_review_chain,
    DROP COLUMN IF EXISTS require_release_deploy,
    DROP COLUMN IF EXISTS require_pipeline_for_review;
