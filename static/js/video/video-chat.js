document.addEventListener("DOMContentLoaded", () => {
    const socket = io({ reconnection: false });
    const videoGrid = document.getElementById("video-grid");
    const myNickname =
        document.getElementById("user-nickname").dataset.nickname;

    const myVideoWrapper = document.createElement("div");
    myVideoWrapper.classList.add("video-wrapper");
    const myVideo = document.createElement("video");
    myVideo.muted = true;
    myVideo.playsInline = true;
    const myNameTag = document.createElement("div");
    myNameTag.classList.add("nickname");
    myNameTag.innerText = `${myNickname} (You)`;
    myVideoWrapper.append(myVideo, myNameTag);
    videoGrid.append(myVideoWrapper);

    let localStream;
    let screenStream;
    let peerConnections = {}; // Stores { pc, candidates: [] }

    const servers = {
        iceServers: [
            {
                urls: [
                    "stun:stun1.l.google.com:19302",
                    "stun:stun2.l.google.com:19302",
                ],
            },
        ],
    };

    // --- Main Setup ---
    async function start() {
        try {
            localStream = await navigator.mediaDevices.getUserMedia({
                video: true,
                audio: {
                    autoGainControl: true,
                    echoCancellation: true,
                },
            });
            myVideo.srcObject = localStream;
            await myVideo.play();
            console.log("Local stream acquired. Joining lounge.");
            socket.emit("join_video_lounge");
        } catch (error) {
            console.error("Error accessing media devices.", error);
            alert("Could not access camera/mic. Please check permissions.");
        }
    }
    start();

    // --- Signaling Handlers ---
    socket.on("all_users", (users) => {
        console.log("Connecting to all existing users:", users);
        users.forEach((user) => handleNewUser(user, false)); // We are NOT the initiator
    });

    socket.on("user_joined_lounge", (user) => {
        console.log("New user joined:", user);
        handleNewUser(user, true); // We ARE the initiator
    });

    socket.on("user_left_lounge", cleanupPeer);
    socket.on("webrtc_offer", handleOffer);
    socket.on("webrtc_answer", handleAnswer);
    socket.on("webrtc_candidate", handleCandidate);

    socket.on("screen_sharing_started", (data) => {
        const videoWrapper = document.getElementById(`video-${data.sid}`);
        if (videoWrapper) {
            videoWrapper.classList.add("screen-sharing");
        }
    });

    socket.on("screen_sharing_stopped", (data) => {
        const videoWrapper = document.getElementById(`video-${data.sid}`);
        if (videoWrapper) {
            videoWrapper.classList.remove("screen-sharing");
        }
    });

    // --- Core Logic Functions ---
    function handleNewUser(user, isInitiator) {
        if (user.sid === socket.id) return;
        const pc = createPeerConnection(user.sid, user.nickname);
        if (isInitiator) {
            createOffer(pc, user.sid);
        }
    }

    async function handleOffer(data) {
        console.log(`Received offer from ${data.senderNickname}`);
        const pc = createPeerConnection(data.senderSid, data.senderNickname);
        await pc.setRemoteDescription(new RTCSessionDescription(data.offer));
        // After setting remote description, process any queued candidates
        await processQueuedCandidates(data.senderSid);

        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        socket.emit("webrtc_answer", {
            answer: answer,
            targetSid: data.senderSid,
        });
    }

    async function handleAnswer(data) {
        console.log(`Received answer from ${data.senderNickname}`);
        const pc = peerConnections[data.senderSid]?.pc;
        if (pc) {
            await pc.setRemoteDescription(
                new RTCSessionDescription(data.answer)
            );
            // After setting remote description, process any queued candidates
            await processQueuedCandidates(data.senderSid);
        }
    }

    function handleCandidate(data) {
        const pc = peerConnections[data.senderSid]?.pc;
        const candidate = new RTCIceCandidate(data.candidate);
        if (pc && pc.remoteDescription) {
            pc.addIceCandidate(candidate);
        } else {
            peerConnections[data.senderSid]?.candidates.push(candidate);
        }
    }

    // **THIS FUNCTION WAS THE MISSING PIECE**
    async function processQueuedCandidates(sid) {
        const connection = peerConnections[sid];
        if (connection && connection.candidates.length > 0) {
            console.log(
                `Processing ${connection.candidates.length} queued candidates for ${sid}`
            );
            for (const candidate of connection.candidates) {
                try {
                    await connection.pc.addIceCandidate(candidate);
                } catch (e) {
                    console.error("Error adding queued ICE candidate:", e);
                }
            }
            connection.candidates = []; // Clear the queue
        }
    }

    function createPeerConnection(targetSid, targetNickname) {
        if (peerConnections[targetSid]) return peerConnections[targetSid].pc;

        const pc = new RTCPeerConnection(servers);
        peerConnections[targetSid] = { pc, candidates: [] };

        localStream
            ?.getTracks()
            .forEach((track) => pc.addTrack(track, localStream));

        const numVideos = Object.keys(peerConnections).length + 1;
        const newEncoding = getBestEncoding(numVideos);
        const sender = pc.getSenders().find((s) => s.track?.kind === "video");
        if (sender) {
            const parameters = sender.getParameters();
            if (!parameters.encodings) {
                parameters.encodings = [{}];
            }
            parameters.encodings[0].maxBitrate = newEncoding.maxBitrate;
            parameters.encodings[0].scaleResolutionDownBy =
                newEncoding.scaleResolutionDownBy;
            sender.setParameters(parameters);
        }

        pc.onicecandidate = (event) => {
            if (event.candidate) {
                socket.emit("webrtc_candidate", {
                    candidate: event.candidate,
                    targetSid: targetSid,
                });
            }
        };

        pc.oniceconnectionstatechange = () => {
            const state = pc.iceConnectionState;
            console.log(`Connection state with ${targetNickname}: ${state}`);
            if (["failed", "disconnected", "closed"].includes(state)) {
                cleanupPeer(targetSid);
            }
        };

        pc.ontrack = (event) => {
            console.log(`Track received from ${targetNickname}`);
            addRemoteVideo(targetSid, targetNickname, event.streams[0]);
        };

        return pc;
    }

    async function createOffer(pc, targetSid) {
        try {
            const offer = await pc.createOffer();
            await pc.setLocalDescription(offer);
            socket.emit("webrtc_offer", {
                offer: pc.localDescription,
                targetSid: targetSid,
            });
        } catch (err) {
            console.error("Error creating offer:", err);
        }
    }

    function addRemoteVideo(sid, nickname, stream) {
        let remoteVideoWrapper = document.getElementById(`video-${sid}`);
        if (remoteVideoWrapper) return;

        remoteVideoWrapper = document.createElement("div");
        remoteVideoWrapper.id = `video-${sid}`;
        remoteVideoWrapper.classList.add("video-wrapper");
        const remoteVideo = document.createElement("video");
        remoteVideo.autoplay = true;
        remoteVideo.playsInline = true;
        remoteVideo.srcObject = stream;
        const remoteNameTag = document.createElement("div");
        remoteNameTag.classList.add("nickname");
        remoteNameTag.innerText = nickname;
        remoteVideoWrapper.append(remoteVideo, remoteNameTag);
        videoGrid.append(remoteVideoWrapper);
    }

    function cleanupPeer(sid) {
        if (peerConnections[sid]) {
            peerConnections[sid].pc.close();
            delete peerConnections[sid];
        }
        const videoElement = document.getElementById(`video-${sid}`);
        if (videoElement) videoElement.remove();
    }

    // --- UI Controls & Final Cleanup ---
    document.getElementById("toggle-mic").addEventListener("click", (event) => {
        const audioTrack = localStream?.getAudioTracks()[0];
        if (audioTrack) {
            audioTrack.enabled = !audioTrack.enabled;
            event.target.textContent = audioTrack.enabled
                ? "Mute Mic"
                : "Unmute Mic";
        }
    });

    function getBestEncoding(numVideos) {
        if (numVideos <= 2) {
            return { maxBitrate: 1500 * 1024, scaleResolutionDownBy: 1.0 }; // High quality
        } else if (numVideos <= 4) {
            return { maxBitrate: 1000 * 1024, scaleResolutionDownBy: 1.5 }; // Medium quality
        } else {
            return { maxBitrate: 500 * 1024, scaleResolutionDownBy: 2.0 }; // Lower quality
        }
    }

    async function startScreenSharing() {
        try {
            screenStream = await navigator.mediaDevices.getDisplayMedia({
                video: true,
            });
            const screenTrack = screenStream.getVideoTracks()[0];

            for (const sid in peerConnections) {
                const pc = peerConnections[sid].pc;
                const sender = pc
                    .getSenders()
                    .find((s) => s.track?.kind === "video");
                if (sender) {
                    sender.replaceTrack(screenTrack);
                }
            }

            myVideo.srcObject = screenStream;
            myVideoWrapper.classList.add("screen-sharing");
            socket.emit("screen_sharing_started");

            document.getElementById("toggle-screen").textContent =
                "Stop Sharing";

            screenTrack.onended = () => {
                stopScreenSharing();
            };
        } catch (error) {
            console.error("Error starting screen sharing:", error);
        }
    }

    function stopScreenSharing() {
        if (screenStream) {
            screenStream.getTracks().forEach((track) => track.stop());
            screenStream = null;
        }

        const localVideoTrack = localStream.getVideoTracks()[0];

        for (const sid in peerConnections) {
            const pc = peerConnections[sid].pc;
            const sender = pc
                .getSenders()
                .find((s) => s.track?.kind === "video");
            if (sender) {
                sender.replaceTrack(localVideoTrack);
            }
        }

        myVideo.srcObject = localStream;
        myVideo.play();
        myVideoWrapper.classList.remove("screen-sharing");
        socket.emit("screen_sharing_stopped");
        document.getElementById("toggle-screen").textContent = "Share Screen";
    }

    document.getElementById("toggle-cam").addEventListener("click", (event) => {
        if (screenStream && screenStream.active) {
            stopScreenSharing();
        } else {
            const videoTrack = localStream?.getVideoTracks()[0];
            if (videoTrack) {
                videoTrack.enabled = !videoTrack.enabled;
                event.target.textContent = videoTrack.enabled
                    ? "Turn Off Cam"
                    : "Turn On Cam";
            }
        }
    });

    document
        .getElementById("toggle-screen")
        .addEventListener("click", (event) => {
            if (screenStream && screenStream.active) {
                // Stop screen sharing
                stopScreenSharing();
            } else {
                // Start screen sharing
                startScreenSharing();
            }
        });

    // window.addEventListener("beforeunload", () => {
    //     socket.disconnect();
    // });
});
