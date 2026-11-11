-- Fresh installs used to be seeded with Turkish. Move the ones that still carry
-- that seed to English; a language picked in Settings rewrote the row after the
-- first boot, so only a row as old as the first settings written is the seed.
UPDATE app_settings
   SET value = 'en'
 WHERE key = 'default_language'
   AND value = 'tr'
   AND updated_at <= (SELECT min(updated_at) FROM app_settings) + interval '1 minute';
