# Grabarr TODO List

## High Priority
- [ ] Add retry logic for network interruptions during large transfers
- [ ] Implement bandwidth monitoring and auto-throttling
- [ ] Add support for excluding certain file types/patterns
- [ ] Create web UI for job management and monitoring

## Medium Priority
- [ ] Create Makefile for build/deployment automation
- [ ] Add configuration validation on startup
- [ ] Implement job scheduling (cron-like functionality)
- [ ] Add disk space cleanup automation (delete old completed downloads)
- [ ] Support for multiple seedbox sources
- [ ] Add progress estimation based on file sizes
- [ ] Implement job dependencies (download B after A completes)

## Low Priority
- [ ] Add statistics/metrics collection
- [ ] Support for different notification backends (Discord, Slack, etc.)
- [ ] Add API authentication/authorization
- [ ] Create Docker health checks that validate external connections
- [ ] Add configuration hot-reload without restart
- [ ] Implement job categories/tagging system

## Bug fixes / Technical debt
- [ ] Add comprehensive error handling for all failure scenarios
- [ ] Improve logging with structured fields
- [ ] Add unit tests for core functionality
- [ ] Optimize database queries and add proper indexing
- [ ] Add input validation for all API endpoints

## Documentation
- [ ] Create comprehensive API documentation
- [ ] Add troubleshooting guide
- [ ] Create deployment guide for different environments
- [ ] Document configuration options in detail