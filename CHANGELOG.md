# Changelog

## [0.4.0](https://github.com/joangrigorov/ghspector/compare/v0.3.0...v0.4.0) (2026-10-13)


### ⚠ BREAKING CHANGES

* **tui:** rebuild the shell on Bubble Tea v2 with a deterministic layout ([#17](https://github.com/joangrigorov/ghspector/issues/17))

### Features

* add PR diff view, details refresh, and handle running job logs gracefully ([#5](https://github.com/joangrigorov/ghspector/issues/5)) ([c40b178](https://github.com/joangrigorov/ghspector/commit/c40b17897b97af90eeb46e42dccd802288d45267))
* **config:** support contextualized workflows and PRs polling intervals in config.yaml ([7201c69](https://github.com/joangrigorov/ghspector/commit/7201c69aedd5aa20b8e951301045663fc376d80c))
* **data:** the GraphQL aggregation upgrade (Phase 5) ([#23](https://github.com/joangrigorov/ghspector/issues/23)) ([f313595](https://github.com/joangrigorov/ghspector/commit/f31359531d1edb89587b8a51a790d4ded0f8c153))
* **prs:** read the whole conversation on the Comments tab ([#27](https://github.com/joangrigorov/ghspector/issues/27)) ([908c752](https://github.com/joangrigorov/ghspector/commit/908c7528ff889ec0c9f6bbd4358ddaca31fe41a2))
* **tui:** add browser links, attempt browsing, actor filters, and help screen ([39e09be](https://github.com/joangrigorov/ghspector/commit/39e09be89ad615f0aca908af82c14b7300fce834))
* **tui:** add read-only issue browser and viewer ([#7](https://github.com/joangrigorov/ghspector/issues/7)) ([306a7d6](https://github.com/joangrigorov/ghspector/commit/306a7d64cdb774aeb82d67e6199e671942a913cb))
* **tui:** add workflow run and deployment approval flow ([#9](https://github.com/joangrigorov/ghspector/issues/9)) ([0fc7c07](https://github.com/joangrigorov/ghspector/commit/0fc7c074782edbea0754f2f2d6aa906e6834ede7))
* **tui:** implement active polling, stable sorting, clock offset sync, log follow mode, actor column, and layout fixes ([777a763](https://github.com/joangrigorov/ghspector/commit/777a76329fd1906f5d3b95cd300ec60cdd539192))
* **tui:** implement custom layout, merge defaults, and repo filtering ([#11](https://github.com/joangrigorov/ghspector/issues/11)) ([2942f60](https://github.com/joangrigorov/ghspector/commit/2942f60378d3fe8a46724d4b59e913e30b135c33))
* **tui:** implement PR details commits browser, checks rollup, layouts, and nil-pointer panic fixes ([ac6a0bf](https://github.com/joangrigorov/ghspector/commit/ac6a0bf2e1ffd060a3e0494db2ecc1647febe82e))
* **tui:** implement the Issues vertical slice (Phase 4) ([#21](https://github.com/joangrigorov/ghspector/issues/21)) ([5b00fb0](https://github.com/joangrigorov/ghspector/commit/5b00fb0412b89406dec022cb74cdf4f3254e8d35))
* **tui:** implement the pull requests vertical slice (Phase 3) ([#19](https://github.com/joangrigorov/ghspector/issues/19)) ([c440e34](https://github.com/joangrigorov/ghspector/commit/c440e347b3bfda01e1d1159fb80352a2d896c7a2))
* **tui:** implement the Workflows vertical slice (Phase 2) ([#18](https://github.com/joangrigorov/ghspector/issues/18)) ([feb73c1](https://github.com/joangrigorov/ghspector/commit/feb73c1ba7973a28fec6cc9930fa389dc987d34a))
* **tui:** initialize ghspector Go Actions TUI ([f85c0de](https://github.com/joangrigorov/ghspector/commit/f85c0de6c73ce90bb81fd9ba0d0429570eb3a586))
* **tui:** rebuild the shell on Bubble Tea v2 with a deterministic layout ([#17](https://github.com/joangrigorov/ghspector/issues/17)) ([6a8a8da](https://github.com/joangrigorov/ghspector/commit/6a8a8da2a64e0454ccdd87a87cc5462daf275e07))
* **tui:** the parity audit and rendered markdown (Phase 6) ([#24](https://github.com/joangrigorov/ghspector/issues/24)) ([127080b](https://github.com/joangrigorov/ghspector/commit/127080bc19ac1bd319fe16b12689d657e3a9fa75))
* **ui:** frame the conversation, wash each contribution and fold resolved threads ([#31](https://github.com/joangrigorov/ghspector/issues/31)) ([d6e4a0b](https://github.com/joangrigorov/ghspector/commit/d6e4a0bb4bb72f68e5949d5b5898ffb55f4dfdd5))


### Bug Fixes

* **deps:** bump github.com/charmbracelet/glamour ([#2](https://github.com/joangrigorov/ghspector/issues/2)) ([d1eda96](https://github.com/joangrigorov/ghspector/commit/d1eda969f44c6c2e8ead064ffc8734ed9e0d9ede))
* **deps:** bump the github-actions-dependencies group across 1 directory with 3 updates ([#14](https://github.com/joangrigorov/ghspector/issues/14)) ([cbf9311](https://github.com/joangrigorov/ghspector/commit/cbf931199f130bd235ed12f55b9e95d88504126e))
* **deps:** upgrade to Go 1.27.2 ([#25](https://github.com/joangrigorov/ghspector/issues/25)) ([49c86eb](https://github.com/joangrigorov/ghspector/commit/49c86eba93ad1e5fe2e531d6590c3c13fed3a596))
* **tui:** one highlight treatment, a palette that follows the terminal ([#20](https://github.com/joangrigorov/ghspector/issues/20)) ([2fca884](https://github.com/joangrigorov/ghspector/commit/2fca884c84ac3e22d40c8745f51465e1f69a5a08))
* **ui:** gather inline threads under their root and link the conversation screens ([#30](https://github.com/joangrigorov/ghspector/issues/30)) ([a886f07](https://github.com/joangrigorov/ghspector/commit/a886f077213401c889b6cc4fdeaa7e65eccd2af8))
* **ui:** give every screen one content box ([#26](https://github.com/joangrigorov/ghspector/issues/26)) ([83f93a7](https://github.com/joangrigorov/ghspector/commit/83f93a79643b9c26c448988c744028fb6c19aec6))

## [0.3.0](https://github.com/joangrigorov/ghspector/compare/v0.2.0...v0.3.0) (2026-07-22)


### Features

* **tui:** implement custom layout, merge defaults, and repo filtering ([#11](https://github.com/joangrigorov/ghspector/issues/11)) ([39e42f1](https://github.com/joangrigorov/ghspector/commit/39e42f196b9cab9009437878c1af4d613e4f1021))

## [0.2.0](https://github.com/joangrigorov/ghspector/compare/v0.1.0...v0.2.0) (2026-07-12)


### Features

* **tui:** add read-only issue browser and viewer ([#7](https://github.com/joangrigorov/ghspector/issues/7)) ([d43887b](https://github.com/joangrigorov/ghspector/commit/d43887ba87d207536987c4b0d0dc2447173b6b4a))
* **tui:** add workflow run and deployment approval flow ([#9](https://github.com/joangrigorov/ghspector/issues/9)) ([6d99114](https://github.com/joangrigorov/ghspector/commit/6d991140f2a3ed4e2180797a2f944248bacf6c67))

## [0.1.0](https://github.com/joangrigorov/ghspector/compare/v0.0.1...v0.1.0) (2026-07-11)


### Features

* add PR diff view, details refresh, and handle running job logs gracefully ([#5](https://github.com/joangrigorov/ghspector/issues/5)) ([f31464f](https://github.com/joangrigorov/ghspector/commit/f31464f0df5bbf727741066e38bede852901533b))

## 0.0.1 (2026-07-10)


### Features

* **config:** support contextualized workflows and PRs polling intervals in config.yaml ([559ec7f](https://github.com/joangrigorov/ghspector/commit/559ec7f6b07b7b4e4176306e9e8050356fdf4b83))
* **tui:** add browser links, attempt browsing, actor filters, and help screen ([bceca23](https://github.com/joangrigorov/ghspector/commit/bceca236a0ecb910555d7ec42489ec2a6ec15522))
* **tui:** implement active polling, stable sorting, clock offset sync, log follow mode, actor column, and layout fixes ([cd90482](https://github.com/joangrigorov/ghspector/commit/cd90482071e629d694e687f6b6e632237e3e8eae))
* **tui:** implement PR details commits browser, checks rollup, layouts, and nil-pointer panic fixes ([1c5a7b3](https://github.com/joangrigorov/ghspector/commit/1c5a7b3726b93ceef30b7a01f86fca74236a9f02))
* **tui:** initialize ghspector Go Actions TUI ([9ea102c](https://github.com/joangrigorov/ghspector/commit/9ea102ce5f249efcaaed942933bd9d96f8f4b1a7))


### Bug Fixes

* **deps:** bump github.com/charmbracelet/glamour ([#2](https://github.com/joangrigorov/ghspector/issues/2)) ([e51d9db](https://github.com/joangrigorov/ghspector/commit/e51d9db579597e7c90bff2ef59844db245daf688))
