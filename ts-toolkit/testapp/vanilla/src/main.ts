import './style.css'

import { FrameClickEvent } from "@readium/navigator-html-injectables/src/modules/ReflowablePeripherals";
import { EpubNavigator, EpubNavigatorListeners } from "@readium/navigator/src/"
import { Locator, Manifest, Publication } from "@readium/shared/src";
import { Fetcher } from "@readium/shared/src/fetcher";
import { HttpFetcher } from "@readium/shared/src/fetcher/HttpFetcher";
import { Link } from "@readium/shared/src";

// Design
import '@material/web/all'; // TODO optimize for elements we use
import Peripherals from './peripherals';
import FrameManager from '@readium/navigator/src/epub/frame/FrameManager';  
import FXLFrameManager from '@readium/navigator/src/epub/fxl/FXLFrameManager';

window.addEventListener("aimagic", () => {
    aimagic()
});
window.addEventListener("enableInput", () => {
    enableInput();
});
window.addEventListener("enableScroll", () => {
    enableScroll();
});
window.addEventListener("toggleDarkMode", () => {
    toggleDarkMode();
});

function enableScroll() {
    let page = getActivePage();
    
    let wrapper = document.getElementById("wrapper");
    if (!wrapper) return;
    wrapper.style.overflow = "scroll";

    let container = document.getElementById("container");
    if (!container) return;
    container.style.height = "200%";

    const pageContent = page?.contentWindow?.document;
    let html = pageContent?.documentElement;
    if (html) {
        let currentStyle = html.getAttribute('style') || '';
        if (!currentStyle.includes('readium-scroll-on')) {
            html.setAttribute('style', currentStyle + ' readium-scroll-on: true;');
        }
    }
}

function toggleDarkMode() {
    let page = getActivePage();
    const pageContent = page?.contentWindow?.document;
    let html = pageContent?.documentElement;
    if (html) {
        let currentStyle = html.getAttribute('style') || '';
        if (!currentStyle.includes('readium-night-on')) {
            html.setAttribute('style', currentStyle + ' readium-night-on: true;');
        } else {
            let newStyle = currentStyle.replace("readium-night-on", "");
            html.setAttribute('style', newStyle);
        }
    }
}

function enableInput() {
    let page = getActivePage();
    const pageContent = page?.contentWindow?.document;
    let body = pageContent?.body;

    const textarea = document.createElement('textarea');
    textarea.id = "story-input";
    textarea.style.width = '100%';
    textarea.style.border = 'none';
    textarea.style.outline = 'none';
    textarea.style.padding = '0';
    textarea.style.minHeight = '40px';
    textarea.style.resize = 'none';
    textarea.style.overflow = 'hidden';
    textarea.style.fontSize = '16px';
    textarea.style.fontFamily = '"Iowan Old Style", "Sitka Text", Palatino, "Book Antiqua", serif';
    textarea.value = "Lily: ";
    textarea.rows = 1;
    
    const autoResize = () => {
        textarea.style.height = 'auto';
        textarea.style.height = textarea.scrollHeight + 'px';
    };

    textarea.addEventListener('input', autoResize);
    autoResize();
    body?.appendChild(textarea);
}

async function aimagic() {
    let page = getActivePage();
    const pageContent = page?.contentWindow?.document;
    let body = pageContent?.body;

    const elements = body?.querySelectorAll('p, textarea');
    const texts: string[] = [];

    elements?.forEach((element) => {
        if (element.tagName.toLowerCase() === 'p') {
            texts.push(element.textContent || '');
        } else if (element.tagName.toLowerCase() === 'textarea') {
            const textareaContent = (element as HTMLTextAreaElement).value;
            texts.push(textareaContent);

            // Create a new paragraph element
            const newParagraph = document.createElement('p');
            newParagraph.textContent = textareaContent;

            // Replace the textarea with the new paragraph
            element.parentNode?.replaceChild(newParagraph, element);
        }
    });

    // const textArea = pageContent?.getElementById("story-input") as HTMLTextAreaElement;
    // texts.push(textArea.value);

    const message = texts.join('\n');
    console.log(message);

    const addedText = document.createElement('p');
    body?.appendChild(addedText);
    try {
        const response = await fetch('http://localhost:5080/api/streamAIResponse', {
            method: 'POST',
            headers: {
            'Content-Type': 'application/json',
            },
            body: JSON.stringify({ message }),
        });
        if (!response.body) { throw new Error('No response body'); }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();

        while (true) {
            const { value, done } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value, { stream: true });
            addedText.textContent = addedText.textContent + chunk;
        }
    } catch (error) {
        console.error('Error:', error);
        addedText.textContent = "An error occured";
    }
}

function getActivePage(): HTMLIFrameElement | undefined {
    let container = document.getElementById("container");
    let iframes = container?.children;
    if (!iframes) return;
    for (let child of iframes) {
        if (child instanceof HTMLIFrameElement && child.checkVisibility()) {
            return child;
        }
    }
    return;
}

const bookid = "dGhlbXlzdGVyeW9mY2FzdGxldHVyaW5nLmVwdWI";

async function load() {
    const currentURL = new URL(window.location.href);
    let book = "http://localhost:5080/" + bookid + "/manifest.json";
    let publicationURL = "http://localhost:5080/" + bookid + "/manifest.json";
    if(currentURL.searchParams.has("book")) {
        book = currentURL.searchParams.get("book")!;
    }
    if(book.startsWith("http://") || book.startsWith("https://")) { // TODO: use URL.canParse()
        publicationURL = book;
        if(!book.endsWith("manifest.json") && !book.endsWith("/")) publicationURL += "/";
    } else
        publicationURL = `${currentURL.origin}/books/${book}/manifest.json`

    const container: HTMLElement = document.body.querySelector("#container") as HTMLElement;
    const manifestLink = new Link({ href: "manifest.json" });
    const fetcher: Fetcher = new HttpFetcher(undefined, publicationURL);
    const fetched = fetcher.get(manifestLink);
    const selfLink = (await fetched.link()).toURL(publicationURL)!;
    await fetched.readAsJSON()
        .then(async (response: unknown) => {
            const manifest = Manifest.deserialize(response as string)!;
            manifest.setSelfLink(selfLink);
            const publication = new Publication({ manifest: manifest, fetcher: fetcher });

            const topBar = document.getElementById("top-bar")!;
            const titleHeader = topBar.querySelector("h3")!;
            titleHeader.innerText = manifest.metadata.title.getTranslation("en");

            const p = new Peripherals({
                moveTo: (direction) => {
                    if (direction === "right") {
                        nav.goRight(true, () => {});
                    } else if(direction === "left") {
                        nav.goLeft(true, () => {});
                    }
                },
                menu: (_show) => {
                    // No UI that hides/shows at the moment
                },
                goProgression: (_shiftKey) => {
                    //nav.goForward(true, () => {});
                }
            });

            const listeners: EpubNavigatorListeners = {
                frameLoaded: function (_wnd: Window): void {
                    /*nav._cframes.forEach((frameManager: FrameManager | FXLFrameManager) => {
                        frameManager.msg!.send(
                            "set_property",
                            ["--USER__colCount", 1],
                            (ok: boolean) => (ok ? {} : {})
                        );
                    })*/
                    nav._cframes.forEach((frameManager: FrameManager | FXLFrameManager | undefined) => {
                        if(frameManager) p.observe(frameManager.window);
                    })
                    p.observe(window);
                },
                positionChanged: function (_locator: Locator): void {
                    window.focus();
                },
                tap: function (_e: FrameClickEvent): boolean {
                    return false;
                },
                click: function (_e: FrameClickEvent): boolean {
                    return false;
                },
                zoom: function (_scale: number): void {
                },
                miscPointer: function (_amount: number): void {
                },
                
                customEvent: function (_key: string, _data: unknown): void {
                },
                handleLocator: function (locator: Locator): boolean {
                    const href = locator.href;
                    if (
                        href.startsWith("http://") ||
                        href.startsWith("https://") ||
                        href.startsWith("mailto:") ||
                        href.startsWith("tel:")
                    ) {
                        if(confirm(`Open "${href}" ?`))
                            window.open(href, "_blank");
                    } else {
                        console.warn("Unhandled locator", locator);
                    }
                    return false;
                }
            }
            const nav = new EpubNavigator(container, publication, listeners);
            await nav.load();

            p.observe(window);

            window.addEventListener("reader-control", (ev) => {
                const detail = (ev as CustomEvent).detail as {
                    command: string;
                    data: unknown;
                };
                switch (detail.command) {
                    case "goRight":
                        nav.goRight(true, () => {});
                        break;
                    case "goLeft":
                        nav.goLeft(true, () => {});
                        break;
                    case "goTo":
                        const link = nav.publication.linkWithHref(detail.data as string);
                        if(!link) {
                            console.error("Link not found", detail.data);
                            return;
                        }
                        nav.goLink(link, true, (ok) => {
                            // Hide TOC dialog if navigation was a success
                            if(ok) (document.getElementById("toc-dialog") as HTMLDialogElement).close();
                        });
                        break;
                    case "settings":
                        (document.getElementById("settings-dialog") as HTMLDialogElement).show();
                        break;
                    case "toc":
                        // Seed TOC
                        const container = document.getElementById("toc-list") as HTMLElement;
                        container.querySelectorAll(":scope > md-list-item, :scope > md-divider").forEach(e => e.remove()); // Clear TOC

                        if(nav.publication.tableOfContents) {
                            const template = container.querySelector("template") as HTMLTemplateElement;
                            nav.publication.tableOfContents.items.forEach(item => {
                                const clone = template.content.cloneNode(true) as HTMLElement;

                                // Link
                                const element = clone.querySelector("md-list-item")!;
                                element.href = `javascript:control('goTo', '${item.href}')`;

                                // Title
                                const headlineSlot = element.querySelector("div[slot=headline]") as HTMLDivElement;
                                headlineSlot.innerText = item.title || "[Untitled]";

                                // Href for debugging
                                const supportingTextSlot = element.querySelector("div[slot=supporting-text]") as HTMLDivElement;
                                supportingTextSlot.innerText = item.href;

                                container.appendChild(clone);
                            })
                        } else {
                            container.innerText = "TOC is empty";
                        }

                        // Show the TOC dialog
                        (document.getElementById("toc-dialog") as HTMLDialogElement).show();
                        break;
                    default:
                        console.error("Unknown reader-control event", ev);
                }
            })

        }).catch((error) => {
            console.error("Error loading manifest", error);
            alert(`Failed loading manifest ${selfLink}`);
        });
}

document.addEventListener("DOMContentLoaded", () => {
    console.log("Document has been loaded");
    load();
});