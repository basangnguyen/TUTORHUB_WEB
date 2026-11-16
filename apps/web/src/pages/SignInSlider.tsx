import { ChevronLeft, ChevronRight, Pause, Play } from "lucide-react";
import {
  useEffect,
  useEffectEvent,
  useRef,
  useState,
  useSyncExternalStore,
  type FocusEvent,
  type KeyboardEvent,
  type PointerEvent,
  type TransitionEvent,
} from "react";
import { flushSync } from "react-dom";

/** Time each slide stays on screen while the slideshow is playing. */
const SLIDE_INTERVAL_MS = 4000;
/** Matches the 560ms track transition plus a small safety margin. */
const LOOP_SETTLE_FALLBACK_MS = 600;
const SWIPE_THRESHOLD_PX = 40;
const SLIDE_WIDTH = 1672;
const SLIDE_HEIGHT = 941;

export interface SignInSlide {
  alt: string;
  src: string;
}

export interface SignInSliderLabels {
  goToSlide: (index: number) => string;
  next: string;
  pause: string;
  play: string;
  previous: string;
  region: string;
  slideFailed: string;
  slidePosition: (current: number, total: number) => string;
}

interface SignInSliderProps {
  labels: SignInSliderLabels;
  slides: readonly SignInSlide[];
}

interface DragState {
  offset: number;
  pointerId: number;
  startX: number;
  width: number;
}

function subscribeVisibility(onChange: () => void) {
  document.addEventListener("visibilitychange", onChange);
  return () => document.removeEventListener("visibilitychange", onChange);
}

function getPageHidden() {
  return document.visibilityState === "hidden";
}

function isFocusLeaving(event: FocusEvent<HTMLElement>) {
  const next = event.relatedTarget;
  return !(next instanceof Node && event.currentTarget.contains(next));
}

/** Track positions are `[clone of last, ...slides, clone of first]`. */
function toSlideIndex(position: number, total: number) {
  return (((position - 1) % total) + total) % total;
}

/**
 * Horizontal, seamlessly looping slideshow for the sign-in hero.
 *
 * It plays on load (including for `prefers-reduced-motion` users, by product
 * decision) and meets WCAG 2.2.2 through the pause control plus automatic
 * pausing on hover, keyboard focus, dragging and hidden browser tabs.
 */
export function SignInSlider({ labels, slides }: SignInSliderProps) {
  const total = slides.length;
  const trackRef = useRef<HTMLDivElement>(null);
  const pointerPressRef = useRef(false);
  const progressRef = useRef({ key: "", elapsed: 0 });
  const [position, setPosition] = useState(1);
  const [animate, setAnimate] = useState(true);
  const [cycle, setCycle] = useState(0);
  const [rotationEnabled, setRotationEnabled] = useState(true);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [drag, setDrag] = useState<DragState | null>(null);
  const [failedSlides, setFailedSlides] = useState<readonly number[]>([]);
  const pageHidden = useSyncExternalStore(
    subscribeVisibility,
    getPageHidden,
    () => false,
  );

  const activeIndex = toSlideIndex(position, total);
  const onClone = position === 0 || position === total + 1;
  const paused =
    !rotationEnabled || hovered || focused || pageHidden || drag !== null;
  const progressKey = `${activeIndex}:${cycle}`;

  /** Jumps from a clone to its real slide without animating; returns it. */
  function settleTrack() {
    const settled = toSlideIndex(position, total) + 1;
    if (settled !== position) {
      flushSync(() => {
        setAnimate(false);
        setPosition(settled);
      });
      // Reading layout commits the instant jump before transitions resume.
      trackRef.current?.getBoundingClientRect();
    }
    return settled;
  }

  function finishLoop() {
    if (settleTrack() !== position) setAnimate(true);
  }

  function step(delta: number) {
    const from = settleTrack();
    setAnimate(true);
    setPosition(from + delta);
  }

  function navigate(delta: number) {
    step(delta);
    setCycle((value) => value + 1);
  }

  function showSlide(index: number) {
    settleTrack();
    setAnimate(true);
    setPosition(index + 1);
    setCycle((value) => value + 1);
  }

  const advance = useEffectEvent(() => step(1));
  const settleAfterLoop = useEffectEvent(() => finishLoop());

  // Auto-advance; pausing keeps the elapsed time so the progress pill and the
  // timer resume together instead of restarting the full interval.
  useEffect(() => {
    if (progressRef.current.key !== progressKey) {
      progressRef.current = { key: progressKey, elapsed: 0 };
    }
    const { elapsed } = progressRef.current;
    if (paused || total < 2) {
      return;
    }
    const startedAt = Date.now();
    const timer = window.setTimeout(
      () => advance(),
      Math.max(0, SLIDE_INTERVAL_MS - elapsed),
    );
    return () => {
      window.clearTimeout(timer);
      progressRef.current = {
        key: progressKey,
        elapsed: elapsed + Date.now() - startedAt,
      };
    };
  }, [paused, progressKey, total]);

  // `transitionend` normally settles a loop; this covers tabs, interrupted
  // transitions and environments that never fire it.
  useEffect(() => {
    if (!onClone) {
      return;
    }
    const timer = window.setTimeout(
      () => settleAfterLoop(),
      LOOP_SETTLE_FALLBACK_MS,
    );
    return () => window.clearTimeout(timer);
  }, [onClone]);

  function handleTransitionEnd(event: TransitionEvent<HTMLDivElement>) {
    if (
      event.target === event.currentTarget &&
      event.propertyName === "transform"
    ) {
      finishLoop();
    }
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
      return;
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") {
      return;
    }
    event.preventDefault();
    setFocused(true);
    navigate(event.key === "ArrowRight" ? 1 : -1);
  }

  function handlePointerDown(event: PointerEvent<HTMLDivElement>) {
    if (drag !== null || total < 2 || event.button !== 0) {
      return;
    }
    settleTrack();
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // Capture is best effort; the drag still works inside the viewport.
    }
    setDrag({
      offset: 0,
      pointerId: event.pointerId,
      startX: event.clientX,
      width: event.currentTarget.clientWidth,
    });
  }

  function handlePointerMove(event: PointerEvent<HTMLDivElement>) {
    if (drag === null || event.pointerId !== drag.pointerId) {
      return;
    }
    const limit = drag.width || Number.POSITIVE_INFINITY;
    const offset = Math.max(
      -limit,
      Math.min(limit, event.clientX - drag.startX),
    );
    setDrag({ ...drag, offset });
  }

  function endDrag(event: PointerEvent<HTMLDivElement>, cancelled: boolean) {
    if (drag === null || event.pointerId !== drag.pointerId) {
      return;
    }
    const offset = event.clientX - drag.startX;
    setDrag(null);
    if (!cancelled && Math.abs(offset) >= SWIPE_THRESHOLD_PX) {
      navigate(offset < 0 ? 1 : -1);
    } else {
      setAnimate(true);
    }
  }

  function markFailed(index: number) {
    setFailedSlides((current) =>
      current.includes(index) ? current : [...current, index],
    );
  }

  const firstSlide = slides[0];
  const lastSlide = slides[total - 1];
  if (firstSlide === undefined || lastSlide === undefined) {
    return null;
  }

  const trackSlides = [
    { slide: lastSlide, slideIndex: total - 1 },
    ...slides.map((slide, slideIndex) => ({ slide, slideIndex })),
    { slide: firstSlide, slideIndex: 0 },
  ];
  const dragOffset = drag?.offset ?? 0;
  const trackClassName = `sign-in-slider__track${
    animate && drag === null ? "" : " is-instant"
  }`;

  return (
    <div
      aria-label={labels.region}
      aria-roledescription="carousel"
      className={`sign-in-slider${paused ? " is-paused" : ""}`}
      onBlur={(event) => {
        if (isFocusLeaving(event)) {
          setFocused(false);
          pointerPressRef.current = false;
        }
      }}
      onFocus={() => {
        // Only keyboard (or programmatic) focus pauses; a mouse click on an
        // arrow should not leave the slideshow stuck once the pointer leaves.
        if (!pointerPressRef.current) setFocused(true);
      }}
      onKeyDown={handleKeyDown}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => {
        setHovered(false);
        pointerPressRef.current = false;
      }}
      onPointerCancelCapture={() => {
        pointerPressRef.current = false;
      }}
      onPointerDownCapture={() => {
        pointerPressRef.current = true;
      }}
      onPointerUpCapture={() => {
        pointerPressRef.current = false;
      }}
      role="region"
      tabIndex={0}
    >
      <div
        aria-live={paused ? "polite" : "off"}
        className={`sign-in-slider__viewport${
          drag === null ? "" : " is-dragging"
        }`}
        onPointerCancel={(event) => endDrag(event, true)}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={(event) => endDrag(event, false)}
      >
        <div
          className={trackClassName}
          onTransitionEnd={handleTransitionEnd}
          ref={trackRef}
          style={{
            transform: `translate3d(calc(${position * -100}% + ${dragOffset}px), 0, 0)`,
          }}
        >
          {trackSlides.map(({ slide, slideIndex }, trackIndex) => {
            const clone = trackIndex === 0 || trackIndex === total + 1;
            const active = !clone && slideIndex === activeIndex;
            const current = trackIndex === position;
            return (
              <div
                aria-hidden={active ? undefined : true}
                aria-label={
                  clone
                    ? undefined
                    : labels.slidePosition(slideIndex + 1, total)
                }
                aria-roledescription={clone ? undefined : "slide"}
                className={`sign-in-slider__slide${
                  current ? " is-current" : ""
                }${clone ? " is-clone" : ""}`}
                key={clone ? `clone-${trackIndex}` : slide.src}
                role={clone ? undefined : "group"}
              >
                {failedSlides.includes(slideIndex) ? (
                  <p className="sign-in-slider__fallback">
                    {labels.slideFailed}
                  </p>
                ) : (
                  <img
                    alt={clone ? "" : slide.alt}
                    decoding="async"
                    draggable={false}
                    fetchPriority={trackIndex === 1 ? "high" : "auto"}
                    height={SLIDE_HEIGHT}
                    onError={() => markFailed(slideIndex)}
                    src={slide.src}
                    width={SLIDE_WIDTH}
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>

      <button
        aria-label={labels.previous}
        className="sign-in-slider__arrow sign-in-slider__arrow--prev"
        onClick={() => navigate(-1)}
        type="button"
      >
        <ChevronLeft aria-hidden="true" />
      </button>
      <button
        aria-label={labels.next}
        className="sign-in-slider__arrow sign-in-slider__arrow--next"
        onClick={() => navigate(1)}
        type="button"
      >
        <ChevronRight aria-hidden="true" />
      </button>

      <div className="sign-in-slider__dots">
        {slides.map((slide, index) => {
          const active = index === activeIndex;
          return (
            <button
              aria-current={active ? "true" : undefined}
              aria-label={labels.goToSlide(index + 1)}
              className={`sign-in-slider__dot${active ? " is-active" : ""}`}
              key={slide.src}
              onClick={() => showSlide(index)}
              type="button"
            >
              <span aria-hidden="true" className="sign-in-slider__dot-mark">
                {active && total > 1 ? (
                  <span
                    className="sign-in-slider__dot-progress"
                    key={progressKey}
                    style={{ animationDuration: `${SLIDE_INTERVAL_MS}ms` }}
                  />
                ) : null}
              </span>
            </button>
          );
        })}
      </div>

      <button
        aria-label={rotationEnabled ? labels.pause : labels.play}
        className="sign-in-slider__rotation"
        onClick={() => setRotationEnabled((value) => !value)}
        type="button"
      >
        {rotationEnabled ? (
          <Pause aria-hidden="true" />
        ) : (
          <Play aria-hidden="true" />
        )}
      </button>
    </div>
  );
}
