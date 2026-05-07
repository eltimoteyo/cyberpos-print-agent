# Print Agent Release Checklist

Use this checklist when preparing a new release of the print-agent.

## Pre-Release (Development)

- [ ] All code changes are committed to git
- [ ] Git tag created (e.g., `git tag -a v0.2.0 -m "Release v0.2.0"`)
- [ ] Code review completed and approved
- [ ] Unit tests pass: `go test ./...` in both print-agent-go and api-gateway
- [ ] Build succeeds on Windows (developer machine)
- [ ] Update README.md with changelog for this version
- [ ] Security audit completed (HMAC keys, rate limits, logging)

## Build & Sign

- [ ] Run build script: `.\print-agent-go\deploy\windows\build-and-sign.ps1`
  - [ ] Build succeeds without errors
  - [ ] Binary size is reasonable (~11 MB)
  - [ ] SHA256 hash generated correctly
  - [ ] Code signing certificate available (if applicable)
  - [ ] Manifest JSON created in `dist/` directory

## Testing

- [ ] Binary runs on Windows 10/11 without errors
- [ ] Binary can be installed via install-agent.ps1
- [ ] Auto-start task is created and works after reboot
- [ ] Update script can detect new version via manifest
- [ ] Update script downloads and verifies SHA256
- [ ] Update script restarts service successfully
- [ ] Print test ticket succeeds on all configured printers
- [ ] Drawer pulse works (if applicable to printer)
- [ ] Paper cut works (if applicable to printer)
- [ ] Rate limiting prevents DoS (test with load tool)
- [ ] Structured JSON logs are generated
- [ ] Health endpoint responds correctly
- [ ] Uninstall script cleanly removes all components

## Pre-Deployment

- [ ] Binary uploaded to CDN or release server
- [ ] SHA256 verified after upload: `wget https://... && certUtil -hashfile ... SHA256`
- [ ] Binary downloadable from public URL
- [ ] Manifest JSON file created with correct SHA256 and URL
- [ ] Release notes prepared and formatted
- [ ] Backup of current live version taken
- [ ] Rollback plan reviewed

## Deployment

- [ ] Manifest values copied to gateway environment variables:
  - [ ] `PRINT_AGENT_VERSION=x.y.z`
  - [ ] `PRINT_AGENT_SHA256=...`
  - [ ] `PRINT_AGENT_INSTALLER_URL=...`
- [ ] Deploy release script run: `.\deploy-release.ps1 ...`
- [ ] api-gateway restarted
- [ ] `GET /api/v1/public/print-agent/latest` returns correct manifest
- [ ] Release notes accessible from manifest

## Post-Deployment Validation

- [ ] Test machine with old version can auto-update
  - [ ] Update script downloads new binary
  - [ ] SHA256 verification passes
  - [ ] Service restarts with new version
  - [ ] Functionality works post-update
- [ ] Monitor logs for errors: `docker logs api-gateway | grep -i print-agent`
- [ ] Monitor logs for errors: `docker logs print-agent`
- [ ] Auto-update task on Windows runs successfully
- [ ] No increase in error rates in monitoring dashboard
- [ ] Customer/partner notifications sent (if applicable)

## Post-Release Monitoring

- [ ] Track auto-update success rate across customer base
- [ ] Monitor error logs for compatibility issues
- [ ] Collect feedback from operations team
- [ ] Review performance metrics (CPU, memory, network)
- [ ] Check for regression in known issues
- [ ] Monitor print job failure rate

## Rollback Criteria

Roll back immediately if:

- [ ] Critical bug found affecting > 10% of installations
- [ ] Security vulnerability discovered
- [ ] Hardware compatibility issue (printer models affected)
- [ ] Performance degradation > 20%
- [ ] Data loss or corruption reported

## Rollback Procedure

If rollback needed:

1. Update gateway env vars back to previous version:
   - `PRINT_AGENT_VERSION=x.y.(z-1)`
   - `PRINT_AGENT_SHA256=<prev_hash>`
   - `PRINT_AGENT_INSTALLER_URL=<prev_url>`

2. Restart api-gateway: `docker restart api-gateway`

3. Verify manifest returns old version: `curl http://gateway/api/v1/public/print-agent/latest`

4. Document root cause in release notes

5. Post-mortem analysis within 24 hours

## Documentation Updates

- [ ] README.md updated with new features/fixes
- [ ] RELEASE_PIPELINE.md updated if process changed
- [ ] SUPPORT_PLAYBOOK_WINDOWS.md updated for new features
- [ ] PHASE0_TECHNICAL_DESIGN.md updated with new API details
- [ ] PRINTING_POS_ARCHITECTURE_PLAN.md updated with Phase 5 status

## Communication

- [ ] Release notes sent to team
- [ ] Known issues documented
- [ ] Update timeline communicated to operations
- [ ] Support contacts notified
- [ ] Customer-facing documentation updated (if applicable)

## Archive

- [ ] Old binary moved to archive storage
- [ ] Build logs archived
- [ ] Release approved by product owner
- [ ] Release tagged in version control: `git tag -a release/v0.2.0-deployed`

---

**Release Version**: __________  
**Release Date**: __________  
**Released By**: __________  
**Approved By**: __________  

## Notes

```
[Space for release notes and observations]




```
