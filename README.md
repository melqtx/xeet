# Xeet (older version)

Simple, beautiful terminal interface for posting to X.com.

```
██╗  ██╗███████╗███████╗████████╗
╚██╗██╔╝██╔════╝██╔════╝╚══██╔══╝
 ╚███╔╝ █████╗  █████╗     ██║   
 ██╔██╗ ██╔══╝  ██╔══╝     ██║   
██╔╝ ██╗███████╗███████╗   ██║   
╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝   

┌────────────────────────────────────────────────────────────┐
│                                                            │
│  |                                                         │
│                                                            │
│  0/280 • Enter to post • Ctrl+C to quit                   │
│                                                            │
└────────────────────────────────────────────────────────────┘
```



## Installation

```bash
git clone https://github.com/melqtx/xeet.git
cd xeet
make install
```

That's it! The `make install` command will build and install xeet to `/usr/local/bin/`.

**After installation, you can use xeet from anywhere:**
```bash
xeet version      # Check version
xeet auth         # Set up X.com credentials  
xeet              # Start tweeting
```

No need to stay in the project directory!

## Quick Start

1. **Set up your X.com API credentials**:
   ```bash
   xeet auth
   ```
   Get your credentials from https://developer.x.com/ (you'll need all 4: API Key, API Secret, Access Token, Access Token Secret)

2. **Start tweeting**:
   ```bash
   xeet
   ```
   That's it! A blue input box appears - type your tweet and hit Enter.

## Usage

### Main Interface
```bash
xeet                 # Opens the tweet input box
```
- Type your tweet (280 character limit)
- Press **Enter** to post
- Press **Ctrl+V** to paste text or images
- Press **Alt+Enter** or **Ctrl+J** for line breaks
- Press **any key** after posting to write another tweet
- Press **Ctrl+C** or **q** to quit

### Authentication
```bash
xeet auth           # Set up X.com API credentials
```

That's it! Only 2 commands to remember.

## X API Setup

1. Go to https://developer.x.com/
2. Create a developer account if you don't have one
3. Create a new app in your developer portal
4. Go to the "Keys and Tokens" tab
5. Generate all required credentials:
   - **API Key** (Consumer Key)
   - **API Secret** (Consumer Secret) 
   - **Access Token**
   - **Access Token Secret**
6. Run `xeet auth` and enter these credentials when prompted

**Note**: You need all 4 credentials. The app will test your credentials automatically after setup.

## uh oh are my keys secured?

- API secrets are encrypted using AES-256-GCM before storage
- Configuration files are stored with restricted permissions (600)
- OAuth 1.0a authentication with X API



## Configuration Files

- Config: `~/.xeet.yaml` (encrypted sensitive data)
- Encryption key: `~/.xeet.key` (auto-generated)


