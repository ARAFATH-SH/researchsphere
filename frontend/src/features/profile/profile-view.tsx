"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { rsButtonClass } from "@/components/ui/button";
import { useSession } from "@/features/auth/session";
import { homeCard } from "@/features/home/cards";

import { AboutDialog } from "./about-dialog";
import { AddPublicationDialog } from "./add-publication-dialog";
import { contactCardButtonId } from "./contact-card";
import { ContactDialog } from "./contact-dialog";
import { ContactViewDialog, hasVisibleContact } from "./contact-view-dialog";
import {
  type Experience,
  getMyProfile,
  getProfile,
  type Language,
  type Profile,
  type ProfileResult,
} from "./api";
import {
  editButtonId,
  type EditSection,
  experienceButtonId,
  isEditSection,
  languageButtonId,
} from "./edit-button";
import { EducationDialog } from "./education-dialog";
import { ExperienceDialog } from "./experience-dialog";
import { ImageDialog } from "./image-dialog";
import { IntroDialog } from "./intro-dialog";
import { LanguageDialog } from "./language-dialog";
import { reloadMyProfile } from "./my-profile";
import { EditPublicationDialog } from "./publication-form";
import { publicationMenuId } from "./publication-menu";
import type { Publication } from "./publications-api";
import { PublicationsSection } from "./publications-section";
import { ProfileHeader } from "./profile-header";
import { ProfileLayout } from "./profile-layout";
import { type EditControls, ProfileSections } from "./profile-sections";
import { ProfileSidebar } from "./profile-sidebar";
import { ProfileSkeleton } from "./profile-skeleton";
import { TagsDialog } from "./tags-dialog";
import { Toast, toastDuration } from "./toast";

// ProfileView shows a researcher's profile (FR-4.5): the signed-in user's own without
// userId, or when userId is their own ID; anyone else's otherwise.
export function ProfileView({ userId }: { userId?: string }) {
  const session = useSession();
  const own =
    userId === undefined ||
    (session.status === "signedIn" && session.user.id === userId);
  const [attempt, setAttempt] = useState(0);
  // The result remembers which request it answers, so a new userId or a retry shows the
  // skeleton until its own answer arrives.
  const key = `${own ? "me" : userId}#${attempt}`;
  const [result, setResult] = useState<{
    key: string;
    value: ProfileResult;
  } | null>(null);

  useEffect(() => {
    let current = true;
    void (own ? getMyProfile() : getProfile(userId ?? "")).then((value) => {
      if (current) setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [own, userId, key]);

  // reload fetches the own profile again after an edit, keeping the page on screen. It
  // goes through the shared store, so the top bar and the home card update too.
  const reload = useCallback(
    (ownId: string) => {
      void reloadMyProfile(ownId).then((value) => {
        if (value.status === "ok") setResult({ key, value });
      });
    },
    [key],
  );

  if (!result || result.key !== key) return <ProfileSkeleton />;
  const r = result.value;
  if (r.status === "notFound") return <NotFound />;
  if (r.status === "error")
    return <LoadFailed onRetry={() => setAttempt((n) => n + 1)} />;

  if (!own) return <OtherProfile profile={r.profile} />;
  const ownId = r.profile.userId;
  return <OwnProfile profile={r.profile} reload={() => reload(ownId)} />;
}

// OtherProfile is someone else's profile. "Contact info" shows only what the API
// returned and is hidden when that is nothing (FR-4.7).
function OtherProfile({ profile }: { profile: Profile }) {
  const [contactOpen, setContactOpen] = useState(false);
  const showContact = hasVisibleContact(profile.contact);
  return (
    <>
      <ProfileLayout
        main={
          <>
            <ProfileHeader
              profile={profile}
              onContact={showContact ? () => setContactOpen(true) : undefined}
            />
            <ProfileSections
              profile={profile}
              publications={<PublicationsSection profile={profile} />}
            />
          </>
        }
        aside={<ProfileSidebar profile={profile} />}
      />
      {contactOpen && (
        <ContactViewDialog
          name={profile.fullName}
          contact={profile.contact}
          onClose={() => setContactOpen(false)}
          returnFocusId={editButtonId("contact")}
        />
      )}
    </>
  );
}

// OwnProfile is the owner's profile with its edit dialogs (FR-4.1 to FR-4.4, FR-4.6, FR-4.8, FR-4.9,
// FR-5.1 to FR-5.3, FR-5.7). A pencil
// opens a section's dialog, and so does /profile?edit=<section>, which the profile
// checklist and the empty-section prompts link to.
function OwnProfile({
  profile,
  reload,
}: {
  profile: Profile;
  reload: () => void;
}) {
  const router = useRouter();
  const params = useSearchParams();
  const requested = params.get("edit");
  const [editing, setEditing] = useState<EditSection | null>(null);
  // experience is the entry the experience dialog edits; null while it adds one.
  const [experience, setExperience] = useState<Experience | null>(null);
  // language is the language the language dialog edits; null while it adds one.
  const [language, setLanguage] = useState<Language | null>(null);
  // publication is the publication the edit dialog changes, outside the ?edit= sections.
  const [publication, setPublication] = useState<Publication | null>(null);
  // publicationsVersion makes the Publications card fetch its list again after a change
  // that leaves the count as it was, such as an edit.
  const [publicationsVersion, setPublicationsVersion] = useState(0);
  // focusAfterClose is an element to focus once a dialog has gone, when the one that
  // opened it is about to disappear, such as a deleted entry's pencil.
  const focusAfterClose = useRef<string | null>(null);
  useEffect(() => {
    const id = focusAfterClose.current;
    if (id === null) return;
    focusAfterClose.current = null;
    document.getElementById(id)?.focus();
  });
  // handled is the ?edit= value whose dialog was already shown, so closing it does not
  // open it again before the URL changes.
  const [handled, setHandled] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(toastTimer.current), []);
  if (requested === null && handled !== null) setHandled(null);
  const open =
    editing ??
    (isEditSection(requested) && handled !== requested ? requested : null);

  function close() {
    setEditing(null);
    setExperience(null);
    setLanguage(null);
    if (requested !== null) {
      setHandled(requested);
      router.replace("/profile", { scroll: false });
    }
  }

  function showToast(message: string) {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), toastDuration);
  }

  function saved(message = "Saved") {
    close();
    reload();
    showToast(message);
  }

  // publicationsChanged refreshes the profile's count and the Publications card.
  function publicationsChanged() {
    reload();
    setPublicationsVersion((n) => n + 1);
  }

  // contactReturnId is the "Contact info" link or the contact card's pencil, whichever
  // opened the contact dialog; the focus goes back to it.
  const [contactReturnId, setContactReturnId] = useState(editButtonId("contact"));
  function editContact(returnId: string) {
    setContactReturnId(returnId);
    setEditing("contact");
  }

  const edit: EditControls = {
    open: setEditing,
    editExperience: (entry) => {
      setExperience(entry);
      setEditing("experience");
    },
    editLanguage: (entry) => {
      setLanguage(entry);
      setEditing("languages");
    },
  };
  const common = {
    profile,
    onClose: close,
    returnFocusId: open ? editButtonId(open) : undefined,
  };
  return (
    <>
      <ProfileLayout
        main={
          <>
            <ProfileHeader
              profile={profile}
              edit={edit}
              onContact={() => editContact(editButtonId("contact"))}
            />
            <ProfileSections
              profile={profile}
              edit={edit}
              publications={
                <PublicationsSection
                  profile={profile}
                  refreshKey={publicationsVersion}
                  controls={{
                    add: () => setEditing("publications"),
                    edit: setPublication,
                    removed: () => {
                      focusAfterClose.current = editButtonId("publications");
                      publicationsChanged();
                      showToast("Removed");
                    },
                  }}
                />
              }
            />
          </>
        }
        aside={
          <ProfileSidebar
            profile={profile}
            onEditContact={() => editContact(contactCardButtonId)}
          />
        }
      />
      {open === "intro" && <IntroDialog {...common} onSaved={() => saved()} />}
      {open === "about" && <AboutDialog {...common} onSaved={() => saved()} />}
      {(open === "interests" || open === "skills") && (
        <TagsDialog key={open} kind={open} {...common} onChanged={reload} />
      )}
      {open === "experience" && (
        <ExperienceDialog
          entry={experience ?? undefined}
          onClose={close}
          onSaved={() => saved()}
          onDeleted={() => {
            focusAfterClose.current = editButtonId("experience");
            saved("Deleted");
          }}
          returnFocusId={
            experience
              ? experienceButtonId(experience.id)
              : editButtonId("experience")
          }
        />
      )}
      {open === "languages" && (
        <LanguageDialog
          entry={language ?? undefined}
          onClose={close}
          onSaved={() => saved()}
          onDeleted={() => {
            focusAfterClose.current = editButtonId("languages");
            saved("Deleted");
          }}
          returnFocusId={
            language ? languageButtonId(language.id) : editButtonId("languages")
          }
        />
      )}
      {open === "publications" && (
        <AddPublicationDialog
          orcidId={profile.orcidId}
          onClose={close}
          onAdded={(message) => {
            setPublicationsVersion((n) => n + 1);
            saved(message);
          }}
          onChanged={publicationsChanged}
          returnFocusId={editButtonId("publications")}
        />
      )}
      {publication && (
        <EditPublicationDialog
          publication={publication}
          onClose={() => setPublication(null)}
          onSaved={() => {
            setPublication(null);
            publicationsChanged();
            showToast("Saved");
          }}
          returnFocusId={publicationMenuId(publication.id)}
        />
      )}
      {open === "education" && <EducationDialog {...common} onSaved={saved} />}
      {open === "contact" && (
        <ContactDialog
          onClose={close}
          onSaved={() => saved()}
          onLinksChanged={reload}
          returnFocusId={contactReturnId}
        />
      )}
      {(open === "photo" || open === "cover") && (
        <ImageDialog
          key={open}
          kind={open}
          name={profile.fullName}
          currentUrl={open === "photo" ? profile.photoUrl : profile.coverUrl}
          onClose={close}
          onChanged={saved}
          returnFocusId={editButtonId(open)}
        />
      )}
      {toast && <Toast message={toast} />}
    </>
  );
}

function NotFound() {
  return (
    <StateCard
      title="Profile not found"
      text="This profile does not exist or is not available."
    >
      <Link href="/home" className={rsButtonClass({ full: true })}>
        Go to Home
      </Link>
    </StateCard>
  );
}

function LoadFailed({ onRetry }: { onRetry: () => void }) {
  return (
    <StateCard
      title="Something went wrong"
      text="We couldn't load this profile."
    >
      <button
        type="button"
        onClick={onRetry}
        className={rsButtonClass({ full: true })}
      >
        Try again
      </button>
    </StateCard>
  );
}

function StateCard({
  title,
  text,
  children,
}: {
  title: string;
  text: string;
  children: ReactNode;
}) {
  return (
    <div
      className={`${homeCard} mx-auto flex w-full max-w-[400px] flex-col gap-4 p-6 text-center`}
    >
      <h1 className="text-rs-h3 leading-rs-tight font-bold text-rs-text">
        {title}
      </h1>
      <p className="text-rs-body text-rs-text-muted">{text}</p>
      {children}
    </div>
  );
}
