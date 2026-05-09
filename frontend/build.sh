if [[ ! -d build ]]; then
	echo "initizing frontend"
	pnpm install
	pnpm build
	exit 0
fi

if [[ $(git diff --name-only -- .) ]]; then
	pnpm build
else
	echo "nothing to do"
fi
