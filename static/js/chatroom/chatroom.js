const form = document.getElementById("form");
const input = document.getElementById("input");
const messages = document.getElementById("messages");

const imageOption = document.getElementById('imageOption');
const imagePreview = document.getElementById('imagePreview');
const botCheckbox  = document.getElementById('botCheckbox');
const botQuestion  = document.getElementById('botQuestion');
const sendImageBtn = document.getElementById('sendImage');
const cancelBtn    = document.getElementById('cancelUpload');
// const uploadProgress = document.getElementById('uploadProgress');



fetch("/get_chatlogs", { credentials: "include" })
  .then(res => res.json())
  .then(data => {
    const renderComplete = [];

    data.forEach(entry => {
      if (
        entry == null ||
        entry.type == null ||
        entry.nickname == null ||
        entry.timestamp == null ||
        (entry.type !== "image" && entry.message == null)
      ) {
        // console.log("Invalid entry:", entry);
        return;
      }

      if (entry.type === "image") {
        const item = document.createElement("li");

        const anchor = document.createElement('a');
        anchor.href = `/get_image/${entry.id}`;
        anchor.target = "_blank";
        anchor.rel = "noopener noreferrer";

        const img = document.createElement('img');
        // img.loading = "lazy";
        img.id = entry.id;
        img.src = `/get_image/${entry.id}`;

        const imageLoad = new Promise((resolve) => {
          // img.onload = resolve;
          img.onload = resolve;
          img.onerror = resolve;
        });

        anchor.appendChild(img);

        item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(entry.nickname)}: </b>`;
        item.appendChild(anchor);
        item.innerHTML += ` <span id="timestamp">${formatTime(entry.timestamp)}</span>`;

        messages.appendChild(item);
        renderComplete.push(imageLoad); // track when image finishes loading

      } else if (entry.type === "highlight") {
        const p = new Promise((resolve) => {
          addHighlightedMessage(entry.message, entry.nickname, entry.timestamp);
          requestAnimationFrame(resolve);
        });
        renderComplete.push(p);

      } else if (entry.type === "system") {
        const p = new Promise((resolve) => {
          addSystemMessage(entry.message, entry.nickname, entry.timestamp);
          requestAnimationFrame(resolve);
        });
        renderComplete.push(p);

      } else {
        const p = new Promise((resolve) => {
          addMessage(entry.message, entry.nickname, entry.timestamp);
          requestAnimationFrame(resolve);
        });
        renderComplete.push(p);
      }
    });

    Promise.all(renderComplete).then(() => {
      // console.log("Maybe")
      requestAnimationFrame(() => {
        // console.log('Forcing scroll after initial load (rAF)'); // For debugging
        scrollToBottom(true); // Now force scroll
      });
      // scrollToBottom(true); // Ensure full scroll after all items load/render
    })
    .catch(error => {
      // It's good practice to catch potential errors from Promise.all
      console.error("Error during initial message rendering:", error);
      // You might still want to attempt a scroll even if some elements failed
      requestAnimationFrame(() => {
        // console.log('Forcing scroll after initial load (rAF in catch)'); // For debugging
        scrollToBottom(true);
      });
  });
});


// fetch("/get_chatlogs", { credentials: "include" })
//   .then(res => res.json())
//   .then(data => {
//     const fragment = document.createDocumentFragment();
//     const imagePromises = [];
//     data.forEach(entry => {
//       if (entry == null ||
//           entry.type == null ||
//           entry.nickname == null ||
//           entry.timestamp == null ||
//           (entry.type !== "image" && entry.message == null)
//         ) {
//         console.log("Invalid entry:", entry);
//         return;
//       }
//       const item = document.createElement("li");
//       if (entry.type === "image") {
//         const anchor = document.createElement('a');
//         anchor.href = `/get_image/${entry.id}`;
//         anchor.target = "_blank";
//         anchor.rel = "noopener noreferrer";

//         // Create <img> element
//         const img = document.createElement('img');
//         img.loading = "lazy";
//         img.id = entry.id;
//         img.src = `/get_image/${entry.id}`;
//         const imagePromise = new Promise((resolve, reject) => {
//           img.onload = resolve;
//           img.onerror = reject;  // Handle image loading errors
//         });

//         // Append the image to the anchor
//         anchor.appendChild(img);

//         // Construct the item
//         item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(entry.nickname)}: </b>`;
//         item.appendChild(anchor);  // Append the anchor (with the image) to the item
//         item.innerHTML += ` <span id="timestamp">${formatTime(entry.timestamp)}</span>`;

//         // item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(entry.nickname)}: </b> <a href="/get_image/${entry.id}" target="_blank" rel="noopener noreferrer" id="${entry.id}"></a> <span id="timestamp">${formatTime(entry.timestamp)}</span>`;

//         imagePromises.push(imagePromise);
//         messages.appendChild(item);
//       } else if (entry.type === "highlight") {
//         addHighlightedMessage(entry.message, entry.nickname, entry.timestamp);
//       } else if (entry.type === "system") {
//         console.log("Adding system message:", entry);
//         addSystemMessage(entry.message, entry.nickname, entry.timestamp);
//       } else {
//         addMessage(entry.message, entry.nickname, entry.timestamp);
//         // const msg = linkify(entry.message);
//         // item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(entry.nickname)}:</b> ${HtmlSanitizer.SanitizeHtml(msg)} <span id="timestamp">${formatTime(entry.timestamp)}</span>`;
//       }
//       // fragment.appendChild(item);
//     });
//     messages.appendChild(fragment);
//     // window.scrollTo(0, document.body.scrollHeight);
//     // Promise.all(imagePromises).then(() => {
//     //   window.scrollTo(0, document.body.scrollHeight);
//     // });
//     Promise.all(imagePromises).then(() => {
//       scrollToBottom(true); // force scroll to bottom after all images load
//     });

//   });


const socket = io({
  query: {
    nickname: getCookie("nickname"),
  }
});


let typing = false;
let lastTypingTime = 0;
const TYPING_TIMER_LENGTH = 2000; // 2 seconds

let missedCount = 0;
const originalTitle = document.title;

function getCookie(name) {
  const cookies = document.cookie.split(";");
  for (let cookie of cookies) {
    let [key, value] = cookie.split("=");
    if (key && key.trim() === name) {
      return value;
    }
  }
  return null;
}

// function scrollToBottom(force = false) {
//   const container = messages;
//   const shouldScroll =
//     force ||
//     Math.abs(container.scrollHeight - container.scrollTop - container.clientHeight) < 200;

//   if (shouldScroll) {
//     container.scrollTop = container.scrollHeight;

//     // backup: scroll whole page too
//     window.scrollTo(0, document.body.scrollHeight);
//   }
// }


// DataTypes
// integers | Numbers | 1, 2, 3, 4, ... -1, -2, -3, ...
// Floats | Decimals | 1.123 
// String | Text | '/" "This this a string" 
// Boolean | True/False | true, false


function scrollToBottom(force = false, minHeight = 200) {
  const atBottom = window.innerHeight + window.scrollY >= document.body.scrollHeight - minHeight;

  
  if (force || atBottom) {
    window.scrollTo({
      top: document.body.scrollHeight,
      behavior: 'smooth'
    });
  }
}


function linkify(text) {
  const markdownLinkRegex = /\[([^\]]+)\]\(([^)]+)\)/gi;
  const urlRegex = /(?:(?:https?|ftp):\/\/)?(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&//=]*)/gi;

  // 1. Convert Markdown links to <a> first
  const html = text.replace(markdownLinkRegex, (_, txt, url) => {
    const href = /^(?:https?|ftp):\/\//i.test(url) ? url : `http://${url}`;
    return `<a href="${href}" target="_blank" rel="noopener noreferrer">${txt}</a>`;
  });

  // 2. Split on <a>…</a> so we don't re-link URLs inside them
  const parts = html.split(/(<a\b[^>]*>[\s\S]*?<\/a>)/gi);

  // 3. Replace URLs only in the non-<a> parts
  for (let i = 0; i < parts.length; i++) {
    if (!parts[i].startsWith('<a')) {
      parts[i] = parts[i].replace(urlRegex, (url) => {
        const href = /^(?:https?|ftp):\/\//i.test(url) ? url : `http://${url}`;
        return `<a href="${href}" target="_blank" rel="noopener noreferrer">${url}</a>`;
      });
    }
  }

  return parts.join('');
}


function formatTime(dateString) {
  const date = new Date(dateString);
  return date.toLocaleTimeString("en-US", {
    hour: "numeric",
    minute: "numeric",
    hour12: true,
  });
}

// function sendImage(file, nickname, timestamp) {
//   const reader = new FileReader();
//   reader.readAsArrayBuffer(file);
//   reader.onload = () => {
//     const arrayBuffer = reader.result;
//     const metadata = { nickname, timestamp, name: file.name };
//     // Send metadata and raw bytes
//     socket.emit('send_image', metadata, arrayBuffer);
//   };
// }

function sendImage(file, nickname, timestamp) {
  const chunkSize = 256 * 1024;  // 256 KB
  const reader = new FileReader();
  reader.onload = () => {
    const buffer = reader.result;
    let offset = 0;
    while (offset < buffer.byteLength) {
      const end = Math.min(offset + chunkSize, buffer.byteLength);
      const chunk = buffer.slice(offset, end);
      socket.emit('image_chunk', {
        id: file.name,                // temporary ID
        chunk: chunk,
        is_last: end === buffer.byteLength,
        metadata: { nickname, timestamp, name: file.name }
      });
      offset = end;
    }
  };
  reader.readAsArrayBuffer(file);
}

function updateUploadProgressUI(sent, total) {
  if (total > 0) {
      // uploadProgress.value = sent;
  }
}

function hideUploadProgress() {
  setTimeout(() => {
        // uploadProgress.style.display = 'none';
        // uploadProgress.value = 0;
  }, 500);
}

function showUploadProgress(totalSize) {
  // uploadProgress.style.display = 'block';
  // uploadProgress.max = totalSize;
  // uploadProgress.value = 0;
  // cancelUploadButton.style.display = 'inline-block'; // Show cancel button if implementing
}

function chunkAndEmit(buffer, id, nickname, timestamp, question=null) {
  const totalSize = buffer.byteLength;
  const chunkSize = 256 * 1024; // 256 KB
  const metadata = { nickname, timestamp, name: id, question: question };
  let offset = 0;
  // showUploadProgress(totalSize);
  while (offset < buffer.byteLength) {
    const end = Math.min(offset + chunkSize, buffer.byteLength);
    const chunk = buffer.slice(offset, end);
    socket.emit('image_chunk', {
      id,
      chunk,
      is_last: end === buffer.byteLength,
      metadata: metadata
    });
    offset = end;
    updateUploadProgressUI(offset, totalSize);
  }
}

async function compressAndSendImage(file, nickname, timestamp, question=null) {

  if (file.type === 'image/gif' || file.type === 'image/svg+xml') {
    const buffer = await file.arrayBuffer();
    return chunkAndEmit(buffer, file.name, nickname, timestamp, question);
  }

  // 1) Load the image into an off‑screen <img>
  const img = await new Promise((res, rej) => {
    const i = new Image();
    i.onload = () => res(i);
    i.onerror = rej;
    i.src = URL.createObjectURL(file);
  });

  // 2) Draw it to a canvas at its original dimensions
  const canvas = document.createElement('canvas');
  canvas.width = img.naturalWidth;
  canvas.height = img.naturalHeight;
  const ctx = canvas.getContext('2d');
  ctx.drawImage(img, 0, 0);

  // 3) Export to a Blob with quality parameter (0–1)
  //    JPEG or WebP; WebP generally smaller but check browser support
  const mimeType = file.type === 'image/png' ? 'image/png' : 'image/jpeg';
  const quality = 0.7; // tweak this: lower = smaller file, more artifacts
  const compressedBlob = await new Promise(res =>
    canvas.toBlob(res, mimeType, quality)
  );

  // 4) Read the compressed Blob as ArrayBuffer
  const buffer = await compressedBlob.arrayBuffer();

  const metadata = { nickname, timestamp, name: file.name, question: question };

  // 5) Chunk & send exactly like your existing sendImage()
  const totalSize = buffer.byteLength;
  const chunkSize = 256 * 1024;
  let offset = 0;
  while (offset < buffer.byteLength) {
    const end = Math.min(offset + chunkSize, buffer.byteLength);
    const chunk = buffer.slice(offset, end);
    socket.emit('image_chunk', {
      id: file.name, // or generate a UUID
      chunk,
      is_last: end === buffer.byteLength,
      metadata: metadata
    });
    offset = end;
    updateUploadProgressUI(offset, totalSize);
  }
}

function updateTitle() {
  document.title =
    missedCount > 0 ? `(${missedCount}) ${originalTitle}` : originalTitle;
}

function addMessage(message, nickname, timestamp) {
  if (!message) return;
  if (!nickname) return;
  if (!timestamp) return;

  const formattedMessage = linkify(message);

  const item = document.createElement("li");
  // Note: Make sure your sanitizer is configured to allow <a> tags, or you might see your anchors stripped out.
  item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(nickname)}:</b> ${HtmlSanitizer.SanitizeHtml(formattedMessage)} <span id="timestamp">${formatTime(timestamp)}</span>`;
  messages.appendChild(item);
  // window.scrollTo(0, document.body.scrollHeight);
  const elementHeight = item.offsetHeight;
  const dynamicThreshold = elementHeight + 200;
  scrollToBottom(false, dynamicThreshold);
}

function addHighlightedMessage(message, nickname, timestamp) {
  if (!message) return;
  if (!nickname) return;
  if (!timestamp) return;

  const formattedMessage = linkify(message);

  const item = document.createElement("li");

  item.classList.add("highlight");
  // Note: Make sure your sanitizer is configured to allow <a> tags, or you might see your anchors stripped out.
  item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(nickname)}:</b> ${HtmlSanitizer.SanitizeHtml(formattedMessage)} <span id="timestamp">${formatTime(timestamp)}</span>`;
  messages.appendChild(item);
  // window.scrollTo(0, document.body.scrollHeight);
  // scrollToBottom();
  const elementHeight = item.offsetHeight;
  const dynamicThreshold = elementHeight + 200;
  scrollToBottom(false, dynamicThreshold);
}

function addSystemMessage(message, nickname, timestamp) {
  if (!message) return;
  if (!nickname) return;
  if (!timestamp) return;

  const formattedMessage = linkify(message);

  const item = document.createElement("li");
  // console.log(`Sending system message: ${formattedMessage}`);
  item.innerHTML = `<b id="nickname">${nickname}:</b> ${formattedMessage} <span id="timestamp">${formatTime(timestamp)}</span>`;
  messages.appendChild(item);
  // window.scrollTo(0, document.body.scrollHeight);
  // scrollToBottom();
  const elementHeight = item.offsetHeight;
  const dynamicThreshold = elementHeight + 200;
  scrollToBottom(false, dynamicThreshold);
}


// function addImageMessage(id, nickname, timestamp) {
//   const item = document.createElement("li");
//   item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(nickname)}:</b> <a href="/get_image/${id}" target="_blank" rel="noopener noreferrer"><img src="/get_image/${id}" /></a> <span id="timestamp">${formatTime(timestamp)}</span>`;
//   messages.appendChild(item);
//   img.onload = () => {
//     window.scrollTo(0, document.body.scrollHeight);
//   };
// }

function openImageOptions(file) {
  // Add size limit (4mb)
  if (file.size > 5 * 1024 * 1024) {
    alert("No image larger than 5mb allowed!\nif you would like to compress your image you can use imagecompressor.com or resizepixel.com");
    return;
  }
  // console.log('Opening image options:);
  imageOption.style.display = 'block';
  // imageOptionInner.style.display = 'flex';
  imagePreview.src = URL.createObjectURL(file);

  // clear old state
  botCheckbox.checked = false;
  botQuestion.value = '';
  
  // stash the file in a closure or global so send handler can pick it up
  imageOption._file = file;
}

function addImageMessage(id, nickname, timestamp) {
  const item = document.createElement("li");
  const img = document.createElement("img"); // Create the <img> element
  // img.loading = "lazy";
  img.id = id;
  img.src = `/get_image/${id}`;
  img.alt = id; // Optional: Set alt text for the image

  // Attach the image to an anchor element
  const anchor = document.createElement("a");
  anchor.href = `/get_image/${id}`;
  anchor.target = "_blank";
  anchor.rel = "noopener noreferrer";
  anchor.appendChild(img);loading="lazy"

  // Set up onload and error handling
  const imagePromise = new Promise((resolve, reject) => {
    img.onload = resolve;
    img.onerror = reject;
  });

  item.innerHTML = `<b id="nickname">${HtmlSanitizer.SanitizeHtml(nickname)}:</b> `;
  item.appendChild(anchor); // Append the anchor (with the image) to the item
  item.innerHTML += ` <span id="timestamp">${formatTime(timestamp)}</span>`;

  messages.appendChild(item);

  imagePromise.then(() => {
    // window.scrollTo(0, document.body.scrollHeight);
    const elementHeight = item.offsetHeight;
    const dynamicThreshold = elementHeight + 50;
    scrollToBottom(false, dynamicThreshold);
  }).catch((error) => {
    console.error('Image failed to load:', error);
    scrollToBottom(false);
  });
}

input.addEventListener('input', () => {
  if (!typing) {
    typing = true;
    socket.emit('typing', { nickname: getCookie('nickname') });
  }
  lastTypingTime = Date.now();

  setTimeout(() => {
    const timeDiff = Date.now() - lastTypingTime;
    if (typing && timeDiff >= TYPING_TIMER_LENGTH) {
      socket.emit('stop_typing', { nickname: getCookie('nickname') });
      typing = false;
    }
  }, TYPING_TIMER_LENGTH);
});

input.addEventListener('blur', () => {
  if (typing) {
    socket.emit('stop_typing', { nickname: getCookie('nickname') });
    typing = false;
  }
});

window.addEventListener("focus", () => {
  missedCount = 0;
  updateTitle();
  scrollToBottom();
});

socket.on("connect", function () {
  console.log("Connection Established");
});

form.addEventListener("submit", (e) => {
  e.preventDefault();
  if (typing) {
    socket.emit('stop_typing', { nickname: getCookie('nickname') });
    typing = false;
  }
  if (input.value === "/cloak") {
    input.value = "";
    cloak();
    return;
  }

  if (input.value === "/refresh" || input.value === "/reload") {
    location.reload();
    return;
  }

  if (input.value === "/gamble") {
    openGame("game-gamble-d6eca0");
    input.value = "";
    return;
  }
  if (input.value) {
    socket.emit("chat_message", {
      message: input.value,
      nickname: getCookie("nickname"),
      timestamp: new Date().toISOString(),
    });
    input.value = "";
  }
});

document.getElementById("openFile").addEventListener("click", function () {
  document.getElementById("fileInput").click();
});

document.getElementById("fileInput").addEventListener("change", function (event) {
  let file = event.target.files[0];
  // sendImage(file, getCookie("nickname"), new Date().toISOString());
  // compressAndSendImage(file, getCookie("nickname"), new Date().toISOString());
  if (file) openImageOptions(file);
});

cancelBtn.addEventListener('click', () => {
  imageOption.style.display = 'none';
  imagePreview.src = '';
  delete imageOption._file;
});

sendImageBtn.addEventListener('click', () => {
  const file = imageOption._file;
  const question = botCheckbox.checked ? botQuestion.value.trim() : null;

  if (!file) return;  // safety

  // show progress bar
  // showUploadProgress(file.size);

  // emit chunks.
  compressAndSendImage(file, getCookie('nickname'), new Date().toISOString(), question);

  // hide overlay
  imageOption.style.display = 'none';
  imagePreview.src = '';
  delete imageOption._file;

  botQuestion.style.display = 'none';
  document.getElementById("fileInput").value = null;
});


socket.on("chat_message", (msg) => {
  if (msg.highlight) {
    addHighlightedMessage(msg.message, msg.nickname, msg.timestamp);
  } else if (msg.system) {
    // console.log("System message sent")
    addSystemMessage(msg.message, msg.nickname, msg.timestamp);
  } else {
    // console.log("Normal message sent")
    addMessage(msg.message, msg.nickname, msg.timestamp);
  }
  if (document.hidden) {
    missedCount++;
    updateTitle();
  }
});

socket.on("clear_chat", () => {
  messages.innerHTML = "";
});

socket.on('add_image', (data) => {
  addImageMessage(data.id, data.nickname, data.timestamp);
  if (document.hidden) {
    missedCount++;
    updateTitle();
  }
});

socket.on("user_connected", (nickname) => {
  const item = document.createElement("li");
  item.innerHTML = `Welcome! <b>${HtmlSanitizer.SanitizeHtml(nickname)}</b> has joined the chat.`;
  messages.appendChild(item);
  // window.scrollTo(0, document.body.scrollHeight);
  scrollToBottom();
});

const typingIndicator = document.getElementById('typing');

socket.on('typing_update', ({ users }) => {
  // users is an array of nicknames currently typing
  if (users.length === 0) {
    typingIndicator.innerHTML = '';
    typingIndicator.style.display = 'none';
    return;
  } else {
    typingIndicator.style.display = 'block';
  }

  let text = '';
  if (users.length === 1) {
    text = `<b>${HtmlSanitizer.SanitizeHtml(users[0])}</b> is typing`;
  } else if (users.length === 2) {
    text = `<b>${HtmlSanitizer.SanitizeHtml(users[0])}</b> and <b>${HtmlSanitizer.SanitizeHtml(users[1])}</b> are typing`;
  } else {
    const last = users.pop();
    // text = `${users.join(', ')}, and ${last} are typing…`;
    text = `${users.map(u => `<b>${HtmlSanitizer.SanitizeHtml(u)}</b>`).join(', ')}, and <b>${HtmlSanitizer.SanitizeHtml(last)}</b> are typing`;

  }

  typingIndicator.innerHTML = `${text}`;
});