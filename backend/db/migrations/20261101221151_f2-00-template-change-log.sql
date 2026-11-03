-- F2-00: the team's own sheet templates sync like the other entities. Global templates
-- (team_id NULL) are written only by catalogue migrations and travel in the Pull snapshot,
-- so the trigger skips them: sync.change_log.team_id is NOT NULL.
CREATE TRIGGER change_log AFTER INSERT OR UPDATE ON setup.sheet_template
  FOR EACH ROW WHEN (NEW.team_id IS NOT NULL) EXECUTE FUNCTION sync.log_change('id');
