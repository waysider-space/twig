{ pkgs, ... }:

{
  packages = [
    pkgs.changie
    pkgs.go-task
    pkgs.curl
    pkgs.gnutar
    pkgs.gzip
    pkgs.zip
  ];

  languages.go = {
    enable = true;
  };
}
