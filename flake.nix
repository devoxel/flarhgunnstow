{
  description = "Go and pnpm development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSystem = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      devShells = forEachSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              nodejs_26
              pnpm
            ];

            # A hook that runs when you enter the shell
            shellHook = ''
              echo "🔨 Welcome to the Go + pnpm dev shell!"
              echo "Go version:   $(go version)"
              echo "Node version: $(node --version)"
              echo "pnpm version: $(pnpm --version)"
            '';
          };
        }
      );
    };
}
