# Template Notes

## Training recording quality

The training room recording settings were changed in `training_room.html` to reduce
`.webm` recording size while keeping usable quality for fitness sessions.

Current settings:

```js
const recordingVideoBitsPerSecond = 1400000;
const recordingAudioBitsPerSecond = 64000;
const adminMediaConstraints = {
  video: {
    width: { ideal: 1280, max: 1280 },
    height: { ideal: 720, max: 720 },
    frameRate: { ideal: 24, max: 24 },
  },
  audio: true,
};
```

The expected size is roughly 660 MB per hour:

```text
1.4 Mbps video + 0.064 Mbps audio = about 1.464 Mbps
```

This should usually be around half the previous storage usage, depending on what
bitrate and resolution the browser selected before.

Original behavior before this change:

```js
localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
mediaRecorder = new MediaRecorder(localStream, mimeType ? { mimeType } : undefined);
```

To restore the old behavior, remove the explicit `recordingVideoBitsPerSecond`,
`recordingAudioBitsPerSecond`, and `adminMediaConstraints` constants, then switch
`getUserMedia(adminMediaConstraints)` and `new MediaRecorder(localStream,
recorderOptions)` back to the original code above.

## Manual admin camera control

The training room now shows a `Turn camera on` / `Turn camera off` button for the
admin broadcaster.

Current behavior:

- Joining the room as admin does not start the camera automatically.
- The admin must click `Turn camera on` before live video starts.
- `Start recording` and `Stop & upload` are hidden while the camera is off.
- Both recording buttons appear when the camera is on. `Stop & upload` is still
  forced disabled during upload/save states.
- Clicking `Turn camera off` stops the local camera stream, closes live viewer
  peer connections, and stops an active recording if one is running.

Previous behavior:

```js
await startAdminMedia();
```

was called from `syncVideoState()` as soon as the admin became the broadcaster,
so the camera opened automatically on room entry.

## Recording list refresh

When a recording is successfully saved, the admin broadcasts a `recording-ready`
signal over the existing room WebSocket. Other clients refresh only the
`Recordings` section by fetching the current training room page and replacing
`#recordings-section`.

This avoids a full page reload, so active video/chat state is not intentionally
reset just to show the new recording.
