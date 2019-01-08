# JIRA Chat Notifier

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.1.1]
### Changed
- URL secret naming - clashes with ACME controller

## [v1.1.0]
### Added
- Support multiple webhook endpoints per project with different configuration
- Per outgoing webhook event configuration
- Configuration of secret via ENV variable
- Hot configuration reload

### Changed
- Skip update events without changelog
- Rewritten configuration file handling (internally)

## [v1.0.0]
### Changed
- Initial version
