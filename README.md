# the Discursive Image Room
This tool acts as orchestrator between the **discursive image** tool suite.

### Read This First
You have to install some dependencies to make this work:
- [ffmpeg](https://ffmpeg.org): for audio transcoding (4.2.2).
- [Go](https://golang.org): for building the dependencies (go1.13.8).
- [redis](https://redis.io) (optional): for caching the image links (999.999.999).
- [yarn](https://classic.yarnpkg.com/en/docs/install/#mac-stable) (optional): to build and serve the [dishow](https://github.com/AndreaKaus/dishow).

**Disclaimer**: `sgtr` is still proprietary software :-( I hope I'll be allowed to free it soon, then I'll add the installation guide from source, at the moment it does not make much sense. The binary is provided though, checkout the release section!

### Installation
Just pick your [release](https://github.com/jecoz/diroom/releases)!

### Usage
Environment variables are used for authenticating with Google.
This code snippet could go inside your .zshrc or .bashrc file:
```
# This is used for authenticating with Google's Speech-To-Text API.
# Create your file https://cloud.google.com/speech-to-text/docs/quickstart-client-libraries#before-you-begin
export GOOGLE_APPLICATION_CREDENTIALS=<.json credentials file path>

# These are used for authenticating and configuring Google's Custom Search API.
# Create yours at https://developers.google.com/custom-search/v1/introduction.
# Creating a custom search allows also to make a search for a selected number of sites,
# we want instead to search the whole internet. Make sure to set **ON** the
# `Image Search` and `Search the entire web` options in the Custom Search control panel.
export GOOGLE_SEARCH_KEY=<your search api key>
export GOOGLE_SEARCH_CX=<your search api cx>
```

The release contains some helper scripts that make life easier to start the tools. Check them
out if you want to understand how they're glued together.

These are the steps required to start a `diroom`:
If you need/want links to be cached (optimized google search usage), start the redis server in
one terminal with:
```
% ./di-cache
```
Now open a new terminal tab (or a new terminal, but of course in the same directory) and start
the server with:
```
% ./di-server --cache
```
If you did not start a redis instance, just drop the `--cache` flag. You now have a websocket server
listening, checkout it's logs to find the port. You can connect to its "/di/stream" path to receive
the stream of links, but first...

(macOS stuff here)
We want now to transcode microphone's input and send it to the server, otherwise we would not have
any input. Run:
```
% ./di-macos-microphone-input
```
Inside `bin` you'll find some useful binaries for connecting (and testing) the websocket
server and for reading streaming transcript raw files (*.strr).
The `echoclient` can be used to connect to the running websocket `dis` server, while the `replayplayer`
can be used to read from the *.strr files at the rate they were written, hence reproducing the speed 
at which the file was written in during the live transcription session.

### The Show :construction:
To consume the data produced by the server properly, `diroom` provides the
[official dishow frontend app](https://github.com/AndreaKaus/dishow) module: it is a React app that
connects to a `dis` websocket and consumes the discursive images it sends, showing them on the screen
for a configurable amount of time, one after the other :tada:.
We will start an HTTP server serving the app on localhost (yes, we're still using the development
environment, we're still working on that):
```
% ./di-show
```
By default the app will be available at `http://localhost:3000`.

### Notes
It is possible to stop the input without affecting the server, and vice-versa.

