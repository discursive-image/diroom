### the Discorsive Image Room
This tool acts as orchestrator between the **discorsive image** tool suite.

#### Notes
Input can be provided through internal microphone on macOS with:
```
ffmpeg -f avfoundation -i ":0" -f mp3 -
```
Note: ":0" is the microphone device. If this causes errors, run
```
ffmpeg -f avfoundation -list_devices true -i ""
```
Which should produce something like
```
(...)
[AVFoundation input device @ 0x7f89cdc012c0] AVFoundation video devices:
[AVFoundation input device @ 0x7f89cdc012c0] [0] FaceTime HD Camera
[AVFoundation input device @ 0x7f89cdc012c0] [1] Capture screen 0
[AVFoundation input device @ 0x7f89cdc012c0] AVFoundation audio devices:
[AVFoundation input device @ 0x7f89cdc012c0] [0] Built-in Microphone
(...)
```
`:0` should correspond to the number of `[0] Built-in Microphone`, in this case `0`.

