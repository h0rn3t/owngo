# shellcheck disable=all
brew update
brew install llvm@22 lld@22 bdw-gc openssl cjson libffi libuv pkg-config
brew link --force --overwrite llvm@22 lld@22 libffi
./install.sh
