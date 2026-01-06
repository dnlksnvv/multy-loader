# Multy Loader

Simple file download manager with web UI. Single binary, no dependencies.

![Screenshot](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)

## Features

- 📦 **Single binary** - just one file, no Node.js/Python/npm needed
- 🌐 **Web UI** - modern dark theme interface
- 📁 **Multiple configs** - organize downloads by projects
- 🔐 **Civitai support** - automatic token handling for civitai.com
- 📊 **Progress tracking** - real-time download progress with speed
- 🔄 **Auto filename detection** - fetches filename from URL headers
- 📝 **Descriptions & sources** - add notes and source links to files

## Installation

### Download Release

1. Go to [Releases](../../releases)
2. Download binary for your OS:
   - `multy-loader-linux-amd64` - Linux
   - `multy-loader-windows-amd64.exe` - Windows
   - `multy-loader-darwin-amd64` - macOS Intel
   - `multy-loader-darwin-arm64` - macOS Apple Silicon
3. Run it!

### Build from Source

```bash
git clone https://github.com/YOUR_USERNAME/multy-loader.git
cd multy-loader
go build -o multy-loader .
./multy-loader
```

## Usage

```bash
# Run with default port (9894)
./multy-loader

# Custom port
PORT=8080 ./multy-loader

# Run in background
nohup ./multy-loader > /dev/null 2>&1 &
```

Open http://localhost:9894 in your browser.

## Configuration

Configs are stored in `configs/` folder next to the binary as JSON files.

### Civitai Token

To download from civitai.com:
1. Get your API token from https://civitai.com/user/account
2. Open config Settings
3. Paste token in "Civitai API Token" field

## License

MIT


