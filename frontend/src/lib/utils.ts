import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs))
}

type WithoutChild<T> = T extends { child?: unknown } ? Omit<T, 'child'> : T
type WithoutChildren<T> = T extends { children?: unknown } ? Omit<T, 'children'> : T
type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>

export type { WithoutChild, WithoutChildren, WithoutChildrenOrChild }

export type WithElementRef<T extends HTMLElement> = T & {
	ref?: HTMLElement | null
}