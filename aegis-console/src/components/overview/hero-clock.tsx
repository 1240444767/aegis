"use client";

import { useEffect, useRef, useState } from "react";
import { Globe2 } from "lucide-react";
import { motion } from "motion/react";
import { browserTimeZone, browserUtcOffset } from "@/lib/datetime";
import { cn } from "@/lib/utils";

const pad = (value: number) => String(value).padStart(2, "0");

/**
 * 概览首屏的时钟：时分大字、秒数小字，每一位都是 motion 弹簧驱动的数字卷轴；
 * 下方是按浏览器时区走的全天刻度轴与时区名。
 */
export function HeroClock({ now, animate }: { now: Date; animate: boolean }) {
  const hours = pad(now.getHours());
  const minutes = pad(now.getMinutes());
  const seconds = pad(now.getSeconds());
  const label = `${hours}:${minutes}:${seconds}`;

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-3" role="timer" aria-label={label}>
        <div aria-hidden className="flex items-center text-5xl leading-none font-semibold tracking-[-0.04em] tabular-nums md:text-6xl">
          <DigitGroup value={hours} animate={animate} />
          <Colon tick={now.getSeconds()} animate={animate} />
          <DigitGroup value={minutes} animate={animate} />
        </div>
        <div aria-hidden className="mb-[0.08em] flex items-center text-2xl leading-none font-medium tracking-[-0.02em] text-muted-foreground tabular-nums md:text-[28px]">
          <DigitGroup value={seconds} animate={animate} />
        </div>
      </div>

      <DayTimeline now={now} animate={animate} />

      <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Globe2 className="size-3.5 shrink-0" />
        <span className="truncate">{browserTimeZone()}</span>
        <span className="rounded border px-1.5 py-px font-data text-[10px] text-foreground/80">{browserUtcOffset(now)}</span>
      </p>
    </div>
  );
}

function DigitGroup({ value, animate }: { value: string; animate: boolean }) {
  return (
    <span className="flex">
      {value.split("").map((char, index) => (
        <DigitReel key={index} digit={Number(char)} animate={animate} />
      ))}
    </span>
  );
}

/** 两个圆点而不是字体里的冒号：位置对准数字的视觉中线，每秒轻轻明暗一次。 */
function Colon({ tick, animate }: { tick: number; animate: boolean }) {
  return (
    <motion.span
      className="mx-[0.1em] flex h-[0.72em] flex-col justify-center gap-[0.2em] self-center"
      animate={animate ? { opacity: tick % 2 === 0 ? 1 : 0.35 } : { opacity: 1 }}
      transition={{ duration: 0.45, ease: "easeInOut" }}
    >
      <span className="size-[0.11em] rounded-full bg-current" />
      <span className="size-[0.11em] rounded-full bg-current" />
    </motion.span>
  );
}

// 末尾多一个 0：9→0 进位时先向上滚到这一格，落定后无动画跳回顶部
const REEL = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0];

/** 一位数字卷轴。列宽由等宽数字自身撑开，不写死宽度。 */
function DigitReel({ digit, animate }: { digit: number; animate: boolean }) {
  const [position, setPosition] = useState(digit);
  const [instant, setInstant] = useState(true);
  const previous = useRef(digit);

  useEffect(() => {
    const from = previous.current;
    previous.current = digit;
    if (from === digit) return;
    const id = requestAnimationFrame(() => {
      setInstant(!animate);
      setPosition(animate && digit < from ? 10 : digit);
    });
    return () => cancelAnimationFrame(id);
  }, [digit, animate]);

  return (
    <span className="relative inline-block h-[1em] overflow-hidden">
      <motion.span
        className="flex flex-col"
        initial={false}
        animate={{ y: `${-position}em` }}
        transition={instant ? { duration: 0 } : { type: "spring", stiffness: 280, damping: 28, mass: 0.8 }}
        onAnimationComplete={() => {
          if (position === 10) {
            setInstant(true);
            setPosition(0);
          }
        }}
      >
        {REEL.map((value, index) => (
          <span key={index} className="flex h-[1em] items-center justify-center">
            {value}
          </span>
        ))}
      </motion.span>
    </span>
  );
}

const TICKS = Array.from({ length: 25 }, (_, hour) => hour);

/** 全天刻度：每小时一格、每六小时长刻度并标时；已过部分填色，指针随时间平滑移动。 */
function DayTimeline({ now, animate }: { now: Date; animate: boolean }) {
  const progress = ((now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds()) / 86_400) * 100;

  return (
    <div aria-hidden className="w-full max-w-[22rem]">
      <div className="relative h-4">
        <div className="absolute inset-x-0 bottom-0 h-px bg-border" />
        <motion.div
          className="absolute bottom-0 left-0 h-px bg-foreground/50"
          initial={false}
          animate={{ width: `${progress}%` }}
          transition={animate ? { duration: 0.8, ease: "linear" } : { duration: 0 }}
        />
        <div className="absolute inset-0 flex items-end justify-between">
          {TICKS.map((hour) => (
            <span
              key={hour}
              className={cn(
                "w-px",
                hour % 6 === 0 ? "h-2.5" : "h-1.5",
                (hour / 24) * 100 <= progress ? "bg-foreground/45" : "bg-border"
              )}
            />
          ))}
        </div>
        <motion.div
          className="absolute -bottom-0.5 -ml-[3px] size-[7px] rounded-full bg-foreground ring-4 ring-foreground/10"
          initial={false}
          animate={{ left: `${progress}%` }}
          transition={animate ? { duration: 0.8, ease: "linear" } : { duration: 0 }}
        />
      </div>
      <div className="mt-1.5 flex justify-between font-data text-[10px] text-muted-foreground/80 tabular-nums">
        {[0, 6, 12, 18, 24].map((hour) => (
          <span key={hour} className={cn(hour === 0 && "-ml-0.5", hour === 24 && "-mr-1")}>
            {pad(hour)}
          </span>
        ))}
      </div>
    </div>
  );
}
