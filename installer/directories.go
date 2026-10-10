package installer

type scriptRoot struct {
	Category    string
	Environment string
	Folder      string
	Suffix      string
}

func directoryDefaults(os string) []scriptRoot {
	if os == "windows" {
		return []scriptRoot{
			{Category: "bin", Folder: "LocalApplicationData", Suffix: `Programs\Twig\bin`},
			{Category: "config", Folder: "ApplicationData"},
			{Category: "data", Folder: "LocalApplicationData"},
		}
	}
	if os == "darwin" {
		return []scriptRoot{
			{Category: "bin", Suffix: ".local/bin"},
			{Category: "config", Suffix: "Library/Application Support"},
			{Category: "data", Suffix: "Library/Application Support"},
		}
	}

	return []scriptRoot{
		{Category: "bin", Suffix: ".local/bin"},
		{Category: "config", Environment: "XDG_CONFIG_HOME", Suffix: ".config"},
		{Category: "data", Environment: "XDG_DATA_HOME", Suffix: ".local/share"},
	}
}
