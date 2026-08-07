const inDangerMode = true; //true; // set to true to harden security

if (
    inDangerMode === true &&
    !(location.href.includes("0.0.0.0") || location.href.includes("localhost"))
) {
    let inFrame;
    try {
        inFrame = window !== top;
    } catch (e) {
        inFrame = true;
    }
    if (!inFrame) {
        // document.body.innerHTML = "";
        if (location.href.includes("admin")) {
            customCloak(
                "https://ssl.gstatic.com/docs/documents/images/kix-favicon-2023q4.ico",
                "Google Docs"
            );
        } else {
            cloak();
        }
    }
    document.title = "New Tab";
}

function cloak() {
    let inFrame;
    try {
        inFrame = window !== top;
    } catch (e) {
        inFrame = true;
    }
    if (!inFrame && !navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = "New Tab";
            const link = popup.document.createElement("link");
            link.rel = "icon";
            // link.href = "https://www.teacherease.com/favicon.ico";
            popup.document.head.appendChild(link);
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            iframe.src = location.href;
            popup.document.body.appendChild(iframe);
            location.replace("https://www.google.com");
        }
    }
}

function openGame(uri) {
    // let inFrame;
    // try {
    //   inFrame = window !== top;
    // } catch (e) {
    //   inFrame = true;
    // }
    if (!navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = "Home - Google Drive";
            const link = popup.document.createElement("link");
            link.rel = "icon";
            link.href =
                "https://ssl.gstatic.com/docs/doclist/images/drive_2022q3_32dp.png";
            popup.document.head.appendChild(link);
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            iframe.src = `https://${location.hostname}/${uri}`;
            iframe.allow = "microphone; camera; fullscreen; autoplay; display-capture";
            popup.document.body.appendChild(iframe);
        }
    }
}

function customCloak(icon, title) {
    let inFrame;
    try {
        inFrame = window !== top;
    } catch (e) {
        inFrame = true;
    }
    if (!inFrame && !navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = title;
            const link = popup.document.createElement("link");
            link.rel = "icon";
            link.href = icon;
            popup.document.head.appendChild(link);
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            iframe.src = location.href;
            iframe.allow = "microphone; camera; fullscreen; autoplay; display-capture";
            popup.document.body.appendChild(iframe);
            location.replace("https://www.google.com");
        }
    }
}

function cloakURI(uri) {
    if (!navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = "Home - Google Drive";
            const link = popup.document.createElement("link");
            link.rel = "icon";
            link.href =
                "https://ssl.gstatic.com/docs/doclist/images/drive_2022q3_32dp.png";
            popup.document.head.appendChild(link);
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            iframe.src = `https://${location.hostname}/${uri}`;
            iframe.allow = "microphone; camera; fullscreen; autoplay; display-capture";
            popup.document.body.appendChild(iframe);
        }
    }
}

function cloakURL(url) {
    if (!navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = "Home - Google Drive";
            const link = popup.document.createElement("link");
            link.rel = "icon";
            link.href =
                "https://ssl.gstatic.com/docs/doclist/images/drive_2022q3_32dp.png";
            popup.document.head.appendChild(link);
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            if (url.startsWith("http://") || url.startsWith("https://")) {
                iframe.src = url;
            } else {
                iframe.src = `https://${url}`;
            }
            iframe.allow = "microphone; camera; fullscreen; autoplay; display-capture";
            popup.document.body.appendChild(iframe);
        }
    }
}

function openCloakedTab(url) {
    if (!navigator.userAgent.includes("Firefox")) {
        const popup = open("about:blank", "_blank");
        if (!popup || popup.closed) {
            document.body.innerHTML = "";
            alert(
                "An unexpected error occured, please try again later.\nError Code 50112"
            );
            location.replace("https://www.google.com");
        } else {
            popup.document.title = "New Tab";
            const iframe = popup.document.createElement("iframe");
            iframe.style.position = "fixed";
            iframe.style.top =
                iframe.style.bottom =
                iframe.style.left =
                iframe.style.right =
                    "0";
            iframe.style.width = iframe.style.height = "100%";
            iframe.style.margin = "0";
            iframe.style.border = iframe.style.outline = "none";
            if (url.startsWith("http://") || url.startsWith("https://")) {
                iframe.src = url;
            } else {
                iframe.src = `https://${url}`;
            }
            iframe.allow = "microphone; camera; fullscreen; autoplay; display-capture";
            popup.document.body.appendChild(iframe);
        }
    }
}
