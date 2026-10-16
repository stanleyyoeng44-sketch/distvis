package main

type Service struct{ l *lock }

func (s *Service) Question(req *SeqMessage, resp *SeqMessage) error {
	s.l.ral.question(req.Seqtotal)
	*resp = SeqMessage{lc.seqtotal()}
	return nil
}

type Application struct{ l *lock }

func (a *Application) Lock(_ *Empty, _ *Empty) error {
	a.l.lock()
	return nil
}

func (a *Application) Unlock(_ *Empty, _ *Empty) error {
	a.l.unlock()
	return nil
}
