# shellcheck disable=all
brew update
brew install llvm@23 lld@23 bdw-gc openssl cjson libffi libuv pkg-config
brew link --force --overwrite llvm@23 lld@23 libffi
./install.sh
