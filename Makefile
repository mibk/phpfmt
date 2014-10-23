source_dir = src/
app_name = phpfmt
phar_name = $(app_name).phar
bin_dir = /usr/local/bin/

.PHONY: compile install

compile:
	@php bin/compile $(phar_name)
	@chmod +x $(phar_name)

install: compile
	@sudo mv $(phar_name) $(bin_dir)$(app_name)
	@echo Installed in $(bin_dir).
