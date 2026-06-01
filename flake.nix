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

          libdave = pkgs.stdenv.mkDerivation {
            pname = "libdave";
            version = "1.1.0";

            # Download the precompiled C library for Ubuntu/Linux x64
            src = pkgs.fetchurl {
              url = "https://github.com/discord/libdave/releases/download/v1.1.1%2Fcpp/libdave-Linux-X64-boringssl.zip";
              hash = "sha256-JHChMdvzmoINiTuj1vATc/xZeGjK/9ajCodG5EHtXt4="; 
            };

            sourceRoot = ".";

            # autoPatchelfHook automatically fixes the dynamic linking (.so) paths!
            nativeBuildInputs = [ pkgs.autoPatchelfHook pkgs.unzip ];

            # Provide standard C++ libraries that the binary might depend on
            buildInputs = [ pkgs.stdenv.cc.cc.lib pkgs.boringssl ];

            installPhase = ''
              mkdir -p $out
              cp -r include $out/
              cp -r lib $out/
            '';
          };

        in
        {
          default = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
              pkg-config
            ];

            buildInputs = [
              libdave 
            ];

            packages = with pkgs; [
              go
              gopls
              nodejs_26
              pnpm
            ];

            CGO_ENABLED = "1";

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
